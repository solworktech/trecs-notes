//go:build linux && !nogtk

#include "gtk_linux.h"
#include <stdlib.h>
#include <string.h>

// ---- the browse window: search, a list of notes, and the selected note, which
// can be edited in place. What a search finds, what a note says and what saving
// does are Go's (internal/notes); this is the window.

enum { B_SEARCH = 1, B_GET = 2, B_SAVE = 3, B_DELETE = 4, B_SYNC = 5, B_STATUS = 6, B_POLL = 7, B_TAGS = 8 };
enum { COL_ID, COL_MARK, COL_DATE, COL_APP, COL_SNIP, NCOLS };

typedef struct {
	GtkWidget *win, *find, *regex, *mcase, *info, *view, *status, *sync;
	GtkWidget *e_app, *e_title, *e_tags, *save, *revert, *del, *right, *created, *server, *server_label;
	GtkListStore *store;
	GtkTreeSelection *sel;
	note_ctx note;
	gint64 cur;        // the note shown, 0 = none
	int dirty, loading;
	int gen, shown_gen;
	gint64 want_id;    // select this note when the list is rebuilt
	int want_row;      // else this row (after a deletion), -1 = neither
	guint find_timer;
} browse;

static browse *B; // there is one window

static char *act(int op, const char *a1, const char *a2, const char *a3, const char *a4, const char *a5) {
	return goBrowseAction(op, (char *)a1, (char *)a2, (char *)a3, (char *)a4, (char *)a5);
}

static void set_status(browse *b, const char *text, int bad) {
	char *esc = g_markup_escape_text(text, -1);
	char *m = g_strdup_printf("<span foreground=\"%s\">%s</span>", bad ? "#c62828" : "#7d8b99", esc);
	gtk_label_set_markup(GTK_LABEL(b->status), m);
	g_free(esc); g_free(m);
}

static void refresh_status(browse *b) {
	char *r = act(B_STATUS, "", "", "", "", "");
	// "attention \x1f text"; attention: there are changes to send
	char *sep = strchr(r, '\x1f');
	if (sep) { *sep = 0; set_status(b, sep + 1, r[0] == '1'); }
	gtk_widget_set_sensitive(b->sync, r[0] == '1');
	free(r);
}

static void set_info(browse *b, const char *text, int bad) {
	char *esc = g_markup_escape_text(text, -1);
	char *m = g_strdup_printf("<span foreground=\"%s\">%s</span>", bad ? "#c62828" : "#7d8b99", esc);
	gtk_label_set_markup(GTK_LABEL(b->info), m);
	g_free(esc); g_free(m);
}

static void update_buttons(browse *b) {
	gboolean has = b->cur != 0;
	gtk_widget_set_sensitive(b->right, has);
	gtk_widget_set_sensitive(b->save, has && b->dirty);
	gtk_widget_set_sensitive(b->revert, has && b->dirty);
	gtk_widget_set_sensitive(b->del, has);
}

static void set_dirty(browse *b, int d) {
	b->dirty = d;
	update_buttons(b);
}

static void on_edited(GtkWidget *w, gpointer d) {
	browse *b = d;
	if (!b->loading) set_dirty(b, 1);
}

static void clear_detail(browse *b) {
	b->loading = 1;
	b->cur = 0;
	gtk_entry_set_text(GTK_ENTRY(b->e_app), "");
	gtk_entry_set_text(GTK_ENTRY(b->e_title), "");
	gtk_entry_set_text(GTK_ENTRY(b->e_tags), "");
	gtk_label_set_text(GTK_LABEL(b->created), "");
	gtk_label_set_text(GTK_LABEL(b->server), "");
	gtk_widget_set_visible(b->server, FALSE);
	gtk_widget_set_visible(b->server_label, FALSE);
	trecs_note_load(&b->note, "", 0);
	b->loading = 0;
	set_dirty(b, 0);
}

// Shows a note in the right-hand side. Reply of B_GET: "app \x1f title \x1f
// tags \x1f saved \x1f server state \x1f markdown(0/1) \x1f text", or "\x1e"
// and a message.
static void load_note(browse *b, gint64 id) {
	char ids[32];
	snprintf(ids, sizeof ids, "%" G_GINT64_FORMAT, id);
	char *r = act(B_GET, ids, "", "", "", "");
	if (r[0] == '\x1e') {
		set_status(b, r + 1, 1);
		clear_detail(b);
		free(r);
		return;
	}
	gchar **p = g_strsplit(r, "\x1f", 7);
	if (g_strv_length(p) == 7) {
		b->loading = 1;
		b->cur = id;
		gtk_entry_set_text(GTK_ENTRY(b->e_app), p[0]);
		gtk_entry_set_text(GTK_ENTRY(b->e_title), p[1]);
		gtk_entry_set_text(GTK_ENTRY(b->e_tags), p[2]);
		gtk_label_set_text(GTK_LABEL(b->created), p[3]);
		gtk_label_set_text(GTK_LABEL(b->server), p[4]);
		gtk_widget_set_visible(b->server, p[4][0] != 0);
		gtk_widget_set_visible(b->server_label, p[4][0] != 0);
		trecs_note_load(&b->note, p[6], atoi(p[5]));
		b->loading = 0;
		set_dirty(b, 0);
	}
	g_strfreev(p);
	free(r);
}

static void start_search(browse *b);

// Asks what to do with unsaved changes. 1 = go on (saved or discarded), 0 = stay.
static int save_note(browse *b, int rebuild);
static int settle_changes(browse *b) {
	if (!b->dirty) return 1;
	GtkWidget *d = gtk_message_dialog_new(GTK_WINDOW(b->win), GTK_DIALOG_MODAL, GTK_MESSAGE_QUESTION,
		GTK_BUTTONS_NONE, "This note has changes that are not saved.");
	gtk_dialog_add_buttons(GTK_DIALOG(d), "_Cancel", GTK_RESPONSE_CANCEL, "_Discard", GTK_RESPONSE_REJECT,
		"_Save", GTK_RESPONSE_ACCEPT, NULL);
	gtk_dialog_set_default_response(GTK_DIALOG(d), GTK_RESPONSE_ACCEPT);
	int resp = gtk_dialog_run(GTK_DIALOG(d));
	gtk_widget_destroy(d);
	if (resp == GTK_RESPONSE_ACCEPT) return save_note(b, 0); // the list is refreshed by the poll
	if (resp == GTK_RESPONSE_REJECT) { b->dirty = 0; return 1; }
	return 0;
}

static gboolean on_select_row(GtkTreeSelection *sel, GtkTreeModel *m, GtkTreePath *path, gboolean cur, gpointer d) {
	// asked before the selection changes: "no" keeps the row where it was
	browse *b = d;
	if (cur || b->loading) return TRUE;
	return settle_changes(b);
}

static void on_selection_changed(GtkTreeSelection *sel, gpointer d) {
	browse *b = d;
	GtkTreeModel *m;
	GtkTreeIter it;
	if (!gtk_tree_selection_get_selected(sel, &m, &it)) return;
	gint64 id;
	gtk_tree_model_get(m, &it, COL_ID, &id, -1);
	if (id != b->cur) load_note(b, id);
}

static void select_row(browse *b, GtkTreeIter *it) {
	GtkTreePath *p = gtk_tree_model_get_path(GTK_TREE_MODEL(b->store), it);
	gtk_tree_selection_select_iter(b->sel, it);
	gtk_tree_view_scroll_to_cell(GTK_TREE_VIEW(b->view), p, NULL, FALSE, 0, 0);
	gtk_tree_path_free(p);
}

// ---- search results arrive from Go's goroutine, one batch at a time

typedef struct { int gen, kind; char *payload; } delivery;

static void add_rows(browse *b, const char *payload) {
	int first = b->shown_gen != b->gen;
	if (first) {
		b->loading = 1; // clearing must not look like a selection or an unsaved note
		gtk_list_store_clear(b->store);
		b->loading = 0;
		b->shown_gen = b->gen;
	}
	int count = gtk_tree_model_iter_n_children(GTK_TREE_MODEL(b->store), NULL);
	gchar **recs = g_strsplit(payload, "\x1e", -1);
	GtkTreeIter want;
	int have_want = 0, have_row = 0;
	GtkTreeIter rowit;
	b->loading = 1; // rows being added is not a person choosing one
	for (int i = 0; recs[i] != NULL; i++) {
		gchar **f = g_strsplit(recs[i], "\x1f", 5);
		if (g_strv_length(f) == 5) {
			GtkTreeIter it;
			gint64 id = g_ascii_strtoll(f[0], NULL, 10);
			gtk_list_store_append(b->store, &it);
			gtk_list_store_set(b->store, &it, COL_ID, id, COL_MARK, f[1], COL_DATE, f[2], COL_APP, f[3], COL_SNIP, f[4], -1);
			if (b->want_id && id == b->want_id) { want = it; have_want = 1; }
			if (b->want_row >= 0 && count == b->want_row) { rowit = it; have_row = 1; }
			count++;
		}
		g_strfreev(f);
	}
	b->loading = 0;
	g_strfreev(recs);
	if (have_want) { b->want_id = 0; select_row(b, &want); }
	else if (have_row) { b->want_row = -1; select_row(b, &rowit); }
	else if (first && b->want_id == 0 && b->want_row < 0 && count > 0) {
		GtkTreeIter it;
		if (gtk_tree_model_get_iter_first(GTK_TREE_MODEL(b->store), &it)) select_row(b, &it);
	}
	char *title = g_strdup_printf("%d note(s)", count);
	gtk_label_set_text(GTK_LABEL(b->info), title);
	g_free(title);
}

static void search_done(browse *b, const char *meta) {
	gchar **p = g_strsplit(meta, "\x1f", 2);
	int capped = p[0] && p[0][0] == '1';
	const char *problem = p[0] && p[1] ? p[1] : "";
	int n = gtk_tree_model_iter_n_children(GTK_TREE_MODEL(b->store), NULL);
	if (problem[0]) {
		set_info(b, problem, 1);
	} else if (b->shown_gen != b->gen) {
		// nothing matched: the list is emptied now
		b->loading = 1;
		gtk_list_store_clear(b->store);
		b->loading = 0;
		b->shown_gen = b->gen;
		clear_detail(b);
		const char *text = gtk_entry_get_text(GTK_ENTRY(b->find))[0] ? "No notes match."
			: "No notes yet. Select some text anywhere and press the hotkey.";
		set_info(b, text, 0);
	} else if (capped) {
		char *t = g_strdup_printf("The first %d matches; narrow the search to see others.", n);
		set_info(b, t, 0);
		g_free(t);
	} else {
		char *t = g_strdup_printf("%d note(s)", n);
		set_info(b, t, 0);
		g_free(t);
	}
	if (!problem[0]) {
		if (b->want_id) { // the note edited no longer matches
			b->want_id = 0;
			GtkTreeIter it;
			if (gtk_tree_model_get_iter_first(GTK_TREE_MODEL(b->store), &it)) select_row(b, &it);
		}
		if (b->want_row >= 0) {
			b->want_row = -1;
			int last = gtk_tree_model_iter_n_children(GTK_TREE_MODEL(b->store), NULL) - 1;
			GtkTreeIter it;
			if (last >= 0 && gtk_tree_model_iter_nth_child(GTK_TREE_MODEL(b->store), &it, NULL, last)) select_row(b, &it);
		}
	}
	g_strfreev(p);
	refresh_status(b);
}

static gboolean deliver_idle(gpointer d) {
	delivery *v = d;
	if (B && v->gen == B->gen) {
		if (v->kind == 0) add_rows(B, v->payload);
		else search_done(B, v->payload);
	}
	g_free(v->payload);
	g_free(v);
	return G_SOURCE_REMOVE;
}

void trecs_browse_deliver(int gen, const char *payload, int kind) {
	delivery *v = g_new0(delivery, 1);
	v->gen = gen;
	v->kind = kind;
	v->payload = g_strdup(payload);
	g_idle_add(deliver_idle, v);
}

static void start_search(browse *b) {
	b->gen++;
	char gen[16];
	snprintf(gen, sizeof gen, "%d", b->gen);
	char *r = act(B_SEARCH, gtk_entry_get_text(GTK_ENTRY(b->find)),
		gtk_toggle_button_get_active(GTK_TOGGLE_BUTTON(b->regex)) ? "1" : "0",
		gtk_toggle_button_get_active(GTK_TOGGLE_BUTTON(b->mcase)) ? "1" : "0", gen, "");
	free(r);
}

static gboolean find_timeout(gpointer d) {
	browse *b = d;
	b->find_timer = 0;
	start_search(b);
	return G_SOURCE_REMOVE;
}

static void schedule_search(browse *b) {
	if (b->find_timer) g_source_remove(b->find_timer);
	b->find_timer = g_timeout_add(120, find_timeout, b);
}

static void on_find_changed(GtkWidget *w, gpointer d) { schedule_search(d); }

// ---- the buttons

static int save_note(browse *b, int rebuild) {
	if (!b->cur) return 1;
	char ids[32];
	snprintf(ids, sizeof ids, "%" G_GINT64_FORMAT, b->cur);
	// the text goes last, apart from the others: it is the one that may hold any character
	char *text = trecs_buffer_text(gtk_text_view_get_buffer(GTK_TEXT_VIEW(b->note.raw)));
	char *fields = g_strjoin("\x1f", gtk_entry_get_text(GTK_ENTRY(b->e_app)), gtk_entry_get_text(GTK_ENTRY(b->e_title)),
		gtk_entry_get_text(GTK_ENTRY(b->e_tags)), NULL);
	char *r = act(B_SAVE, ids, fields, text, "", "");
	g_free(text); g_free(fields);
	int ok = r[0] == '1';
	set_status(b, r + 2, !ok);
	free(r);
	if (ok) {
		set_dirty(b, 0);
		if (rebuild) {
			b->want_id = b->cur;
			b->cur = 0; // shown afresh: its sync state has changed
			start_search(b);
		}
	}
	return ok;
}

static void on_save(GtkButton *x, gpointer d) { save_note(d, 1); }

static void on_revert(GtkButton *x, gpointer d) {
	browse *b = d;
	gint64 id = b->cur;
	b->cur = 0;
	load_note(b, id);
}

static void delete_note(browse *b) {
	if (!b->cur) return;
	GtkWidget *d = gtk_message_dialog_new(GTK_WINDOW(b->win), GTK_DIALOG_MODAL, GTK_MESSAGE_QUESTION,
		GTK_BUTTONS_NONE, "Delete this note?");
	gtk_message_dialog_format_secondary_text(GTK_MESSAGE_DIALOG(d), "%s",
		"It cannot be brought back. If syncing is on, its copy on the server is deleted too.");
	gtk_dialog_add_buttons(GTK_DIALOG(d), "_Cancel", GTK_RESPONSE_CANCEL, "_Delete", GTK_RESPONSE_ACCEPT, NULL);
	gtk_dialog_set_default_response(GTK_DIALOG(d), GTK_RESPONSE_CANCEL);
	int resp = gtk_dialog_run(GTK_DIALOG(d));
	gtk_widget_destroy(d);
	if (resp != GTK_RESPONSE_ACCEPT) return;
	char ids[32];
	snprintf(ids, sizeof ids, "%" G_GINT64_FORMAT, b->cur);
	char *r = act(B_DELETE, ids, "", "", "", "");
	int ok = r[0] == '1';
	set_status(b, r + 2, !ok);
	free(r);
	if (!ok) return;
	// select what takes its place
	int row = -1;
	GtkTreeModel *m;
	GtkTreeIter it;
	if (gtk_tree_selection_get_selected(b->sel, &m, &it)) {
		GtkTreePath *p = gtk_tree_model_get_path(m, &it);
		row = gtk_tree_path_get_indices(p)[0];
		gtk_tree_path_free(p);
	}
	set_dirty(b, 0);
	b->cur = 0;
	b->want_row = row;
	start_search(b);
}

static void on_delete(GtkButton *x, gpointer d) { delete_note(d); }

static void on_sync(GtkButton *x, gpointer d) {
	browse *b = d;
	set_status(b, "Sending...", 0);
	while (gtk_events_pending()) gtk_main_iteration();
	char *r = act(B_SYNC, "", "", "", "", "");
	set_status(b, r + 2, r[0] != '1');
	free(r);
	b->want_id = b->cur;
	start_search(b);
}

static gboolean on_view_key(GtkWidget *w, GdkEventKey *e, gpointer d) {
	if (e->keyval == GDK_KEY_Delete || e->keyval == GDK_KEY_KP_Delete) {
		delete_note(d);
		return TRUE;
	}
	return FALSE;
}

static gboolean on_window_key(GtkWidget *w, GdkEventKey *e, gpointer d) {
	browse *b = d;
	guint mods = e->state & gtk_accelerator_get_default_mod_mask();
	if ((mods & GDK_CONTROL_MASK) && (e->keyval == GDK_KEY_f || e->keyval == GDK_KEY_F)) {
		gtk_widget_grab_focus(b->find);
		return TRUE;
	}
	if ((mods & GDK_CONTROL_MASK) && (e->keyval == GDK_KEY_s || e->keyval == GDK_KEY_S)) {
		if (b->dirty) save_note(b, 1);
		return TRUE;
	}
	if (e->keyval == GDK_KEY_Escape) {
		if (gtk_widget_has_focus(b->find) && gtk_entry_get_text(GTK_ENTRY(b->find))[0]) {
			gtk_entry_set_text(GTK_ENTRY(b->find), "");
		} else if (gtk_widget_has_focus(b->find)) {
			gtk_widget_grab_focus(b->view);
		} else {
			gtk_widget_grab_focus(b->find);
		}
		return TRUE;
	}
	return FALSE;
}

static gboolean on_close(GtkWidget *w, GdkEvent *e, gpointer d) {
	browse *b = d;
	if (!settle_changes(b)) return TRUE; // stay
	return FALSE;
}

// notices writes made behind the window's back (the tray saving a note, a
// background sync finishing)
static gboolean poll(gpointer d) {
	browse *b = d;
	char *r = act(B_POLL, "", "", "", "", "");
	int changed = r[0] == '1';
	free(r);
	if (changed && !b->dirty) {
		b->want_id = b->cur;
		start_search(b);
	}
	return G_SOURCE_CONTINUE;
}


static void add_column(GtkWidget *view, const char *title, int col, int expand, const char *colour, int width) {
	GtkCellRenderer *r = gtk_cell_renderer_text_new();
	g_object_set(r, "ellipsize", PANGO_ELLIPSIZE_END, NULL);
	if (colour) g_object_set(r, "foreground", colour, NULL);
	GtkTreeViewColumn *c = gtk_tree_view_column_new_with_attributes(title, r, "text", col, NULL);
	gtk_tree_view_column_set_expand(c, expand);
	if (width > 0) { gtk_tree_view_column_set_fixed_width(c, width); gtk_tree_view_column_set_sizing(c, GTK_TREE_VIEW_COLUMN_FIXED); }
	gtk_tree_view_append_column(GTK_TREE_VIEW(view), c);
}

// a labelled row of the note's grid; *label_out gets the label, to hide with its value
static GtkWidget *labelled(const char *label, GtkWidget *w, GtkWidget *grid, int row, GtkWidget **label_out) {
	GtkWidget *l = gtk_label_new(label);
	gtk_widget_set_halign(l, GTK_ALIGN_START);
	gtk_widget_set_margin_start(l, 12);
	gtk_grid_attach(GTK_GRID(grid), l, 0, row, 1, 1);
	gtk_widget_set_hexpand(w, TRUE);
	gtk_grid_attach(GTK_GRID(grid), w, 1, row, 1, 1);
	if (label_out) *label_out = l;
	return w;
}

int trecs_browse(const browse_in *in) {
	if (!gtk_init_check(NULL, NULL)) return -1;
	browse *b = g_new0(browse, 1);
	B = b;
	b->want_row = -1;

	b->win = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	trecs_apply_header(b->win, "Trecs Notes", in->icon, in->iconlen);
	gtk_window_set_default_size(GTK_WINDOW(b->win), in->width > 0 ? in->width : 1000, in->height > 0 ? in->height : 620);
	gtk_window_set_position(GTK_WINDOW(b->win), GTK_WIN_POS_CENTER);

	GtkWidget *top = gtk_box_new(GTK_ORIENTATION_VERTICAL, 8);
	gtk_container_set_border_width(GTK_CONTAINER(top), 10);
	gtk_container_add(GTK_CONTAINER(b->win), top);

	// search row
	GtkWidget *bar = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 10);
	b->find = gtk_search_entry_new();
	gtk_entry_set_placeholder_text(GTK_ENTRY(b->find), "words, \"a phrase\", tag:x app:y since:2026-01-31 until:...");
	gtk_widget_set_tooltip_text(b->find, "Whole words (the last may be a beginning); \"a phrase\"; tag:  app:  since:  until:");
	b->regex = gtk_check_button_new_with_mnemonic("_Regex");
	gtk_widget_set_tooltip_text(b->regex, "The text is a regular expression");
	b->mcase = gtk_check_button_new_with_mnemonic("Match _case");
	gtk_widget_set_tooltip_text(b->mcase, "Off: case counts only if the text has a capital letter");
	b->info = gtk_label_new("");
	gtk_label_set_ellipsize(GTK_LABEL(b->info), PANGO_ELLIPSIZE_END);
	gtk_widget_set_halign(b->info, GTK_ALIGN_START);
	gtk_box_pack_start(GTK_BOX(bar), b->find, TRUE, TRUE, 0);
	gtk_box_pack_start(GTK_BOX(bar), b->regex, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(bar), b->mcase, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(top), bar, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(top), b->info, FALSE, FALSE, 0);

	// list
	b->store = gtk_list_store_new(NCOLS, G_TYPE_INT64, G_TYPE_STRING, G_TYPE_STRING, G_TYPE_STRING, G_TYPE_STRING);
	b->view = gtk_tree_view_new_with_model(GTK_TREE_MODEL(b->store));
	gtk_tree_view_set_headers_visible(GTK_TREE_VIEW(b->view), FALSE);
	gtk_tree_view_set_enable_search(GTK_TREE_VIEW(b->view), FALSE);
	add_column(b->view, "", COL_MARK, 0, "#d1423f", 28);
	add_column(b->view, "Saved", COL_DATE, 0, "#7d8b99", 130);
	add_column(b->view, "App", COL_APP, 0, "#3b78c4", 110);
	add_column(b->view, "Note", COL_SNIP, 1, NULL, 0);
	b->sel = gtk_tree_view_get_selection(GTK_TREE_VIEW(b->view));
	gtk_tree_selection_set_mode(b->sel, GTK_SELECTION_SINGLE);
	gtk_tree_selection_set_select_function(b->sel, on_select_row, b, NULL);
	GtkWidget *lsw = gtk_scrolled_window_new(NULL, NULL);
	gtk_scrolled_window_set_shadow_type(GTK_SCROLLED_WINDOW(lsw), GTK_SHADOW_IN);
	gtk_container_add(GTK_CONTAINER(lsw), b->view);
	gtk_widget_set_size_request(lsw, 380, -1);

	// the note
	b->right = gtk_box_new(GTK_ORIENTATION_VERTICAL, 8);
	GtkWidget *grid = gtk_grid_new();
	gtk_grid_set_row_spacing(GTK_GRID(grid), 6);
	gtk_grid_set_column_spacing(GTK_GRID(grid), 10);
	b->e_app = labelled("App", gtk_entry_new(), grid, 0, NULL);
	b->e_title = labelled("Title", gtk_entry_new(), grid, 1, NULL);
	b->e_tags = labelled("Tags", gtk_entry_new(), grid, 2, NULL);
	gtk_entry_set_placeholder_text(GTK_ENTRY(b->e_tags), "space or comma separated");
	b->created = gtk_label_new("");
	gtk_widget_set_halign(b->created, GTK_ALIGN_START);
	labelled("Saved", b->created, grid, 3, NULL);
	b->server = gtk_label_new("");
	gtk_widget_set_halign(b->server, GTK_ALIGN_START);
	labelled("Server", b->server, grid, 4, &b->server_label);
	gtk_widget_set_no_show_all(b->server, TRUE); // shown only when syncing is on
	gtk_widget_set_no_show_all(b->server_label, TRUE);
	gtk_box_pack_start(GTK_BOX(b->right), grid, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(b->right), trecs_note_area(&b->note, ""), TRUE, TRUE, 0);
	b->del = gtk_button_new_with_mnemonic("_Delete");
	b->revert = gtk_button_new_with_mnemonic("Re_vert");
	b->save = gtk_button_new_with_mnemonic("_Save");
	gtk_style_context_add_class(gtk_widget_get_style_context(b->save), "suggested-action");
	gtk_style_context_add_class(gtk_widget_get_style_context(b->del), "destructive-action");

	GtkWidget *paned = gtk_paned_new(GTK_ORIENTATION_HORIZONTAL);
	gtk_paned_pack1(GTK_PANED(paned), lsw, FALSE, FALSE);
	gtk_paned_pack2(GTK_PANED(paned), b->right, TRUE, FALSE);
	gtk_widget_set_vexpand(paned, TRUE);
	gtk_box_pack_start(GTK_BOX(top), paned, TRUE, TRUE, 0);

	// the bottom row: the status, then every button
	GtkWidget *foot = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
	b->status = gtk_label_new("");
	gtk_widget_set_halign(b->status, GTK_ALIGN_START);
	gtk_label_set_ellipsize(GTK_LABEL(b->status), PANGO_ELLIPSIZE_END);
	b->sync = gtk_button_new_with_mnemonic("Sync to ser_ver");
	gtk_widget_set_tooltip_text(b->sync, "Send the server what it has not seen: new notes, edits and deletions");
	gtk_box_pack_start(GTK_BOX(foot), b->status, TRUE, TRUE, 0);
	gtk_box_pack_start(GTK_BOX(foot), b->del, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(foot), b->revert, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(foot), b->save, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(foot), gtk_separator_new(GTK_ORIENTATION_VERTICAL), FALSE, FALSE, 4);
	gtk_box_pack_start(GTK_BOX(foot), b->sync, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(top), foot, FALSE, FALSE, 0);

	// tag completion from the tags in use
	char *tags = act(B_TAGS, "", "", "", "", "");
	if (tags[0]) {
		gchar **t = g_strsplit(tags, "\x1f", -1);
		trecs_attach_tag_completion(b->e_tags, (const char **)t, g_strv_length(t));
		g_strfreev(t); // the completion copied them
	}
	free(tags);

	g_signal_connect(b->find, "search-changed", G_CALLBACK(on_find_changed), b);
	g_signal_connect(b->regex, "toggled", G_CALLBACK(on_find_changed), b);
	g_signal_connect(b->mcase, "toggled", G_CALLBACK(on_find_changed), b);
	g_signal_connect(b->sel, "changed", G_CALLBACK(on_selection_changed), b);
	g_signal_connect(b->view, "key-press-event", G_CALLBACK(on_view_key), b);
	g_signal_connect(b->e_app, "changed", G_CALLBACK(on_edited), b);
	g_signal_connect(b->e_title, "changed", G_CALLBACK(on_edited), b);
	g_signal_connect(b->e_tags, "changed", G_CALLBACK(on_edited), b);
	g_signal_connect(gtk_text_view_get_buffer(GTK_TEXT_VIEW(b->note.raw)), "changed", G_CALLBACK(on_edited), b);
	g_signal_connect(b->save, "clicked", G_CALLBACK(on_save), b);
	g_signal_connect(b->revert, "clicked", G_CALLBACK(on_revert), b);
	g_signal_connect(b->del, "clicked", G_CALLBACK(on_delete), b);
	g_signal_connect(b->sync, "clicked", G_CALLBACK(on_sync), b);
	g_signal_connect(b->win, "key-press-event", G_CALLBACK(on_window_key), b);
	g_signal_connect(b->win, "delete-event", G_CALLBACK(on_close), b);
	g_signal_connect(b->win, "destroy", G_CALLBACK(gtk_main_quit), NULL);

	clear_detail(b);
	refresh_status(b);
	char *r = act(B_POLL, "", "", "", "", ""); // the first look: only what changes after it counts
	free(r);
	g_timeout_add_seconds(2, poll, b);

	gtk_widget_show_all(b->win);
	start_search(b);
	gtk_widget_grab_focus(b->view);
	gtk_window_present(GTK_WINDOW(b->win));
	gtk_main();

	B = NULL;
	if (b->find_timer) g_source_remove(b->find_timer);
	trecs_note_free(&b->note);
	g_free(b);
	return 0;
}
