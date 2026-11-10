//go:build linux && !nogtk

#include "gtk_linux.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>


// Tags are space/comma separated; completion works on the word being typed.
static gboolean tag_match(GtkEntryCompletion *c, const gchar *key, GtkTreeIter *iter, gpointer d) {
	const gchar *word = key + strlen(key);
	while (word > key && word[-1] != ' ' && word[-1] != ',') word--;
	if (*word == '\0') return FALSE;
	GtkTreeModel *m = gtk_entry_completion_get_model(c);
	gchar *tag = NULL;
	gtk_tree_model_get(m, iter, 0, &tag, -1);
	gboolean ok = tag != NULL && g_str_has_prefix(tag, word) && strcmp(tag, word) != 0;
	g_free(tag);
	return ok;
}

static gboolean tag_selected(GtkEntryCompletion *c, GtkTreeModel *m, GtkTreeIter *iter, gpointer entry) {
	gchar *tag = NULL;
	gtk_tree_model_get(m, iter, 0, &tag, -1);
	const gchar *text = gtk_entry_get_text(GTK_ENTRY(entry));
	const gchar *cut = text + strlen(text);
	while (cut > text && cut[-1] != ' ' && cut[-1] != ',') cut--;
	gchar *prefix = g_strndup(text, cut - text);
	gchar *nt = g_strconcat(prefix, tag, " ", NULL);
	gtk_entry_set_text(GTK_ENTRY(entry), nt);
	gtk_editable_set_position(GTK_EDITABLE(entry), -1);
	g_free(prefix); g_free(nt); g_free(tag);
	return TRUE;
}

// Completion of the tag being typed, from the tags already in use.
void trecs_attach_tag_completion(GtkWidget *e_tags, const char **sugg, int nsugg) {
	if (nsugg <= 0) return;
	GtkListStore *store = gtk_list_store_new(1, G_TYPE_STRING);
	for (int i = 0; i < nsugg; i++) {
		GtkTreeIter it;
		gtk_list_store_append(store, &it);
		gtk_list_store_set(store, &it, 0, sugg[i], -1);
	}
	GtkEntryCompletion *comp = gtk_entry_completion_new();
	gtk_entry_completion_set_model(comp, GTK_TREE_MODEL(store));
	gtk_entry_completion_set_text_column(comp, 0);
	gtk_entry_completion_set_match_func(comp, tag_match, NULL, NULL);
	gtk_entry_completion_set_minimum_key_length(comp, 1);
	g_signal_connect(comp, "match-selected", G_CALLBACK(tag_selected), e_tags);
	gtk_entry_set_completion(GTK_ENTRY(e_tags), comp);
}

// A client-side header bar instead of the window manager's title bar: the
// WM decides title alignment (xfwm4 left-aligns), a GtkHeaderBar always
// centres it, and it can carry the icon on every desktop.
void trecs_apply_header(GtkWidget *dlg, const char *title, const unsigned char *icon, int iconlen) {
	GtkWidget *hb = gtk_header_bar_new();
	gtk_header_bar_set_title(GTK_HEADER_BAR(hb), title);
	gtk_header_bar_set_show_close_button(GTK_HEADER_BAR(hb), TRUE);
	// Some desktops' button layout has an "icon" element (e.g. "icon:close"):
	// GTK then draws the window icon itself, on windows that are not dialogs,
	// and ours would be a second one. Take it out of the layout we use.
	gchar *layout = NULL;
	g_object_get(gtk_settings_get_default(), "gtk-decoration-layout", &layout, NULL);
	if (layout != NULL && strstr(layout, "icon") != NULL) {
		gchar **side = g_strsplit(layout, ":", 2);
		for (int i = 0; side[i] != NULL; i++) {
			gchar **items = g_strsplit(side[i], ",", -1);
			GString *kept = g_string_new("");
			for (int j = 0; items[j] != NULL; j++) {
				if (strcmp(items[j], "icon") == 0 || items[j][0] == 0) continue;
				if (kept->len) g_string_append_c(kept, ',');
				g_string_append(kept, items[j]);
			}
			g_free(side[i]);
			side[i] = g_string_free(kept, FALSE);
			g_strfreev(items);
		}
		gchar *fixed = g_strdup_printf("%s:%s", side[0], side[1] != NULL ? side[1] : "");
		gtk_header_bar_set_decoration_layout(GTK_HEADER_BAR(hb), fixed);
		g_free(fixed);
		g_strfreev(side);
	}
	g_free(layout);
	if (icon != NULL && iconlen > 0) {
		GdkPixbufLoader *ld = gdk_pixbuf_loader_new();
		if (gdk_pixbuf_loader_write(ld, icon, iconlen, NULL) && gdk_pixbuf_loader_close(ld, NULL)) {
			GdkPixbuf *pb = gdk_pixbuf_loader_get_pixbuf(ld);
			if (pb != NULL) {
				gtk_window_set_icon(GTK_WINDOW(dlg), pb); // taskbar / alt-tab
				GdkPixbuf *small = gdk_pixbuf_scale_simple(pb, 22, 22, GDK_INTERP_BILINEAR);
				gtk_header_bar_pack_start(GTK_HEADER_BAR(hb), gtk_image_new_from_pixbuf(small)); // title bar
				g_object_unref(small);
			}
		}
		g_object_unref(ld);
	}
	gtk_window_set_titlebar(GTK_WINDOW(dlg), hb);
}


// ---- the note area: a Raw tab (editable, with syntax colours) and a Markdown
// tab (rendered preview). Both are fed by Go (goRender, internal/render).

enum { OP_HIGHLIGHT = 1, OP_MARKDOWN = 2, OP_SYNTAXES = 3, OP_FENCE_HINT = 4 };
enum { NCOLORS = 10 };
enum { F_BOLD = 1, F_ITALIC = 2, F_STRIKE = 4, F_CODE = 8, F_LINK = 16, F_QUOTE = 32,
       F_CODEBLOCK = 64, F_H1 = 128, F_H2 = 256, F_H3 = 512, COLOR_SHIFT = 12 };

// mid-tones that read on both light and dark themes; index = render.Color
static const char *color_hex[NCOLORS] = {
	NULL, "#7d8b99", "#3f9142", "#00838f", "#a64ca6", "#c08a00", "#3b78c4", "#d1423f", "#d9731a", "#8a8f98",
};


static void add_color_tags(GtkTextBuffer *b) {
	char name[8];
	for (int i = 1; i < NCOLORS; i++) {
		GdkRGBA c;
		gdk_rgba_parse(&c, color_hex[i]);
		snprintf(name, sizeof name, "c%d", i);
		GtkTextTag *t = gtk_text_buffer_create_tag(b, name, "foreground-rgba", &c, NULL);
		if (i == 1) g_object_set(t, "style", PANGO_STYLE_ITALIC, NULL);
	}
}

static void add_preview_tags(GtkTextBuffer *b) {
	GdkRGBA shade, gray, blue;
	gdk_rgba_parse(&shade, "rgba(127,127,127,0.20)");
	gdk_rgba_parse(&gray, "#7d8b99");
	gdk_rgba_parse(&blue, "#3b78c4");
	gtk_text_buffer_create_tag(b, "bold", "weight", PANGO_WEIGHT_BOLD, NULL);
	gtk_text_buffer_create_tag(b, "italic", "style", PANGO_STYLE_ITALIC, NULL);
	gtk_text_buffer_create_tag(b, "strike", "strikethrough", TRUE, NULL);
	gtk_text_buffer_create_tag(b, "code", "family", "monospace", "background-rgba", &shade, NULL);
	gtk_text_buffer_create_tag(b, "link", "foreground-rgba", &blue, "underline", PANGO_UNDERLINE_SINGLE, NULL);
	gtk_text_buffer_create_tag(b, "quote", "left-margin", 16, "foreground-rgba", &gray, "style", PANGO_STYLE_ITALIC, NULL);
	gtk_text_buffer_create_tag(b, "codeblock", "paragraph-background-rgba", &shade, "left-margin", 8, "right-margin", 8, NULL);
	gtk_text_buffer_create_tag(b, "h1", "weight", PANGO_WEIGHT_BOLD, "scale", 1.6, NULL);
	gtk_text_buffer_create_tag(b, "h2", "weight", PANGO_WEIGHT_BOLD, "scale", 1.35, NULL);
	gtk_text_buffer_create_tag(b, "h3", "weight", PANGO_WEIGHT_BOLD, "scale", 1.15, NULL);
	add_color_tags(b);
}

static char *render(int op, const char *a, const char *b) {
	return goRender(op, (char *)a, (char *)b);
}

char *trecs_buffer_text(GtkTextBuffer *b) {
	GtkTextIter s, e;
	gtk_text_buffer_get_bounds(b, &s, &e);
	return gtk_text_buffer_get_text(b, &s, &e, FALSE);
}

static void update_syntax_label(note_ctx *c) {
	char *label;
	if (c->choice[0] == 0)
		label = c->auto_name && c->auto_name[0] ? g_strdup_printf("Auto (%s)", c->auto_name) : g_strdup("Auto");
	else if (strcmp(c->choice, "none") == 0)
		label = g_strdup("None");
	else
		label = g_strdup(c->choice);
	gtk_button_set_label(GTK_BUTTON(c->syn_button), label);
	g_free(label);
}

static void schedule_highlight(note_ctx *c, guint ms);

static void choose_syntax(note_ctx *c, const char *value) {
	g_free(c->choice);
	c->choice = g_strdup(value);
	update_syntax_label(c);
	schedule_highlight(c, 10);
}

static void add_syntax_row(note_ctx *c, const char *label, const char *value) {
	GtkWidget *row = gtk_list_box_row_new();
	GtkWidget *l = gtk_label_new(label);
	gtk_widget_set_halign(l, GTK_ALIGN_START);
	gtk_widget_set_margin_start(l, 6);
	gtk_widget_set_margin_end(l, 6);
	gtk_widget_set_margin_top(l, 3);
	gtk_widget_set_margin_bottom(l, 3);
	gtk_container_add(GTK_CONTAINER(row), l);
	g_object_set_data_full(G_OBJECT(row), "value", g_strdup(value), g_free);
	g_object_set_data_full(G_OBJECT(row), "label", g_utf8_strdown(label, -1), g_free);
	gtk_container_add(GTK_CONTAINER(c->syn_list), row);
}

static void load_syntaxes(note_ctx *c) {
	if (c->syntaxes_loaded) return;
	c->syntaxes_loaded = 1;
	char *list = render(OP_SYNTAXES, "", "");
	add_syntax_row(c, "Auto", "");
	add_syntax_row(c, "None", "none");
	gchar **names = g_strsplit(list, "\x1f", -1);
	for (int i = 0; names[i] != NULL; i++)
		if (names[i][0]) add_syntax_row(c, names[i], names[i]);
	g_strfreev(names);
	free(list);
	gtk_widget_show_all(c->syn_list);
}

static gboolean syntax_filter(GtkListBoxRow *row, gpointer d) {
	note_ctx *c = d;
	const char *q = gtk_entry_get_text(GTK_ENTRY(c->syn_search));
	if (q[0] == 0) return TRUE;
	char *lq = g_utf8_strdown(q, -1);
	gboolean hit = strstr((const char *)g_object_get_data(G_OBJECT(row), "label"), lq) != NULL;
	g_free(lq);
	return hit;
}

static void on_syntax_search(GtkSearchEntry *e, gpointer d) {
	note_ctx *c = d;
	gtk_list_box_invalidate_filter(GTK_LIST_BOX(c->syn_list));
}

static void on_syntax_row(GtkListBox *box, GtkListBoxRow *row, gpointer d) {
	note_ctx *c = d;
	gtk_popover_popdown(GTK_POPOVER(c->syn_popover));
	choose_syntax(c, (const char *)g_object_get_data(G_OBJECT(row), "value"));
}

// Enter in the search box takes the first match
static void on_syntax_enter(GtkEntry *e, gpointer d) {
	note_ctx *c = d;
	for (int i = 0;; i++) {
		GtkListBoxRow *row = gtk_list_box_get_row_at_index(GTK_LIST_BOX(c->syn_list), i);
		if (row == NULL) return;
		if (gtk_widget_get_child_visible(GTK_WIDGET(row))) {
			on_syntax_row(GTK_LIST_BOX(c->syn_list), row, c);
			return;
		}
	}
}

static void on_syntax_popup(GtkWidget *w, gpointer d) {
	note_ctx *c = d;
	load_syntaxes(c);
	gtk_entry_set_text(GTK_ENTRY(c->syn_search), "");
	gtk_widget_grab_focus(c->syn_search);
}

static void highlight_raw(note_ctx *c) {
	GtkTextBuffer *b = gtk_text_view_get_buffer(GTK_TEXT_VIEW(c->raw));
	char *text = trecs_buffer_text(b);
	char *reply = render(OP_HIGHLIGHT, text, c->choice);
	GtkTextIter s, e;
	char name[8];

	gtk_text_buffer_get_bounds(b, &s, &e);
	for (int i = 1; i < NCOLORS; i++) {
		snprintf(name, sizeof name, "c%d", i);
		gtk_text_buffer_remove_tag_by_name(b, name, &s, &e);
	}
	// reply: resolved language, a record separator, then "start:end:color;..."
	char *sep = strchr(reply, '\x1e');
	if (sep != NULL) {
		*sep = 0;
		if (c->choice[0] == 0) {
			g_free(c->auto_name);
			c->auto_name = g_strdup(reply);
			update_syntax_label(c);
		}
		gchar **spans = g_strsplit(sep + 1, ";", -1);
		for (int i = 0; spans[i] != NULL; i++) {
			int st, en, col;
			if (sscanf(spans[i], "%d:%d:%d", &st, &en, &col) != 3 || col < 1 || col >= NCOLORS) continue;
			GtkTextIter is, ie;
			gtk_text_buffer_get_iter_at_offset(b, &is, st);
			gtk_text_buffer_get_iter_at_offset(b, &ie, en);
			snprintf(name, sizeof name, "c%d", col);
			gtk_text_buffer_apply_tag_by_name(b, name, &is, &ie);
		}
		g_strfreev(spans);
	}
	g_free(text); free(reply);
}

static gboolean highlight_timeout(gpointer d) {
	note_ctx *c = d;
	c->timer = 0;
	if (!c->hint_done) {
		c->hint_done = 1;
		// first run: a fence on the first line ("```go") names the language
		GtkTextBuffer *b = gtk_text_view_get_buffer(GTK_TEXT_VIEW(c->raw));
		char *text = trecs_buffer_text(b);
		char *hint = render(OP_FENCE_HINT, text, "");
		if (hint[0]) {
			g_free(c->choice);
			c->choice = g_strdup(hint);
			update_syntax_label(c);
		}
		g_free(text); free(hint);
	}
	highlight_raw(c);
	return G_SOURCE_REMOVE;
}

static void schedule_highlight(note_ctx *c, guint ms) {
	if (c->timer) g_source_remove(c->timer);
	c->timer = g_timeout_add(ms, highlight_timeout, c);
}

static void on_raw_changed(GtkTextBuffer *b, gpointer d) { schedule_highlight(d, 200); }

static void add_flag_tag(GtkTextBuffer *b, const char *tag, int from, int to) {
	GtkTextIter s, e;
	gtk_text_buffer_get_iter_at_offset(b, &s, from);
	gtk_text_buffer_get_iter_at_offset(b, &e, to);
	gtk_text_buffer_apply_tag_by_name(b, tag, &s, &e);
}

static void render_preview(note_ctx *c) {
	GtkTextBuffer *src = gtk_text_view_get_buffer(GTK_TEXT_VIEW(c->raw));
	GtkTextBuffer *b = gtk_text_view_get_buffer(GTK_TEXT_VIEW(c->preview));
	char *text = trecs_buffer_text(src);
	char *reply = render(OP_MARKDOWN, text, "");
	gtk_text_buffer_set_text(b, "", -1);

	gchar **runs = g_strsplit(reply, "\x1e", -1);
	for (int i = 0; runs[i] != NULL; i++) {
		char *sep = strchr(runs[i], '\x1f');
		if (sep == NULL) continue;
		*sep = 0;
		int bits = atoi(runs[i]);
		const char *chunk = sep + 1;
		int from = gtk_text_buffer_get_char_count(b);
		GtkTextIter end;
		gtk_text_buffer_get_end_iter(b, &end);
		gtk_text_buffer_insert(b, &end, chunk, -1);
		int to = gtk_text_buffer_get_char_count(b);
		if (bits & F_BOLD) add_flag_tag(b, "bold", from, to);
		if (bits & F_ITALIC) add_flag_tag(b, "italic", from, to);
		if (bits & F_STRIKE) add_flag_tag(b, "strike", from, to);
		if (bits & F_CODE) add_flag_tag(b, "code", from, to);
		if (bits & F_LINK) add_flag_tag(b, "link", from, to);
		if (bits & F_QUOTE) add_flag_tag(b, "quote", from, to);
		if (bits & F_CODEBLOCK) add_flag_tag(b, "codeblock", from, to);
		if (bits & F_H1) add_flag_tag(b, "h1", from, to);
		if (bits & F_H2) add_flag_tag(b, "h2", from, to);
		if (bits & F_H3) add_flag_tag(b, "h3", from, to);
		int col = bits >> COLOR_SHIFT;
		if (col >= 1 && col < NCOLORS) {
			char name[8];
			snprintf(name, sizeof name, "c%d", col);
			add_flag_tag(b, name, from, to);
		}
	}
	g_strfreev(runs);
	g_free(text); free(reply);
}

static void on_switch_page(GtkNotebook *nb, GtkWidget *page, guint num, gpointer d) {
	note_ctx *c = d;
	if (c->quiet) return;
	if (num == 1) {
		render_preview(c);
	} else {
		gtk_widget_grab_focus(c->raw);
	}
}

GtkWidget *trecs_scrolled(GtkWidget *child) {
	GtkWidget *sw = gtk_scrolled_window_new(NULL, NULL);
	gtk_scrolled_window_set_shadow_type(GTK_SCROLLED_WINDOW(sw), GTK_SHADOW_IN);
	gtk_container_add(GTK_CONTAINER(sw), child);
	gtk_widget_set_vexpand(sw, TRUE);
	return sw;
}

// builds the Raw / Markdown notebook; *raw_out is the editable view
GtkWidget *trecs_note_area(note_ctx *c, const char *text) {
	c->raw = gtk_text_view_new();
	gtk_text_view_set_wrap_mode(GTK_TEXT_VIEW(c->raw), GTK_WRAP_WORD_CHAR);
	gtk_text_view_set_monospace(GTK_TEXT_VIEW(c->raw), TRUE);
	GtkTextBuffer *rb = gtk_text_view_get_buffer(GTK_TEXT_VIEW(c->raw));
	add_color_tags(rb);
	gtk_text_buffer_set_text(rb, text, -1);

	c->preview = gtk_text_view_new();
	gtk_text_view_set_wrap_mode(GTK_TEXT_VIEW(c->preview), GTK_WRAP_WORD_CHAR);
	gtk_text_view_set_editable(GTK_TEXT_VIEW(c->preview), FALSE);
	gtk_text_view_set_cursor_visible(GTK_TEXT_VIEW(c->preview), FALSE);
	gtk_text_view_set_left_margin(GTK_TEXT_VIEW(c->preview), 6);
	gtk_text_view_set_right_margin(GTK_TEXT_VIEW(c->preview), 6);
	add_preview_tags(gtk_text_view_get_buffer(GTK_TEXT_VIEW(c->preview)));

	c->notebook = gtk_notebook_new();
	gtk_notebook_append_page(GTK_NOTEBOOK(c->notebook), trecs_scrolled(c->raw), gtk_label_new("Raw"));
	gtk_notebook_append_page(GTK_NOTEBOOK(c->notebook), trecs_scrolled(c->preview), gtk_label_new("Markdown"));
	gtk_widget_set_vexpand(c->notebook, TRUE);

	// the syntax chooser sits at the right end of the tab row: a button that
	// opens a popover with a search box over a scrolling list
	c->choice = g_strdup("");
	c->syn_button = gtk_menu_button_new();
	gtk_button_set_label(GTK_BUTTON(c->syn_button), "Auto");
	gtk_widget_set_tooltip_text(c->syn_button, "Syntax colours for the Raw tab");
	c->syn_popover = gtk_popover_new(c->syn_button);
	GtkWidget *pbox = gtk_box_new(GTK_ORIENTATION_VERTICAL, 6);
	gtk_container_set_border_width(GTK_CONTAINER(pbox), 6);
	c->syn_search = gtk_search_entry_new();
	c->syn_list = gtk_list_box_new();
	gtk_list_box_set_filter_func(GTK_LIST_BOX(c->syn_list), syntax_filter, c, NULL);
	GtkWidget *lsw = gtk_scrolled_window_new(NULL, NULL);
	gtk_scrolled_window_set_max_content_height(GTK_SCROLLED_WINDOW(lsw), 260);
	gtk_scrolled_window_set_propagate_natural_height(GTK_SCROLLED_WINDOW(lsw), TRUE);
	gtk_scrolled_window_set_min_content_width(GTK_SCROLLED_WINDOW(lsw), 220);
	gtk_container_add(GTK_CONTAINER(lsw), c->syn_list);
	gtk_box_pack_start(GTK_BOX(pbox), c->syn_search, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(pbox), lsw, TRUE, TRUE, 0);
	gtk_widget_show_all(pbox);
	gtk_container_add(GTK_CONTAINER(c->syn_popover), pbox);
	gtk_menu_button_set_popover(GTK_MENU_BUTTON(c->syn_button), c->syn_popover);
	gtk_widget_show(c->syn_button);
	gtk_notebook_set_action_widget(GTK_NOTEBOOK(c->notebook), c->syn_button, GTK_PACK_END);

	g_signal_connect(c->syn_search, "search-changed", G_CALLBACK(on_syntax_search), c);
	g_signal_connect(c->syn_search, "activate", G_CALLBACK(on_syntax_enter), c);
	g_signal_connect(c->syn_list, "row-activated", G_CALLBACK(on_syntax_row), c);
	g_signal_connect(c->syn_popover, "show", G_CALLBACK(on_syntax_popup), c);
	g_signal_connect(rb, "changed", G_CALLBACK(on_raw_changed), c);
	g_signal_connect(c->notebook, "switch-page", G_CALLBACK(on_switch_page), c);
	schedule_highlight(c, 30); // after the window is up: loading the lexers takes a moment
	return c->notebook;
}

// Puts another note in the area: its language and tab are chosen afresh
// (markdown: show the Markdown tab). It does not take the focus.
void trecs_note_load(note_ctx *c, const char *text, int markdown) {
	c->quiet = 1;
	g_free(c->choice);
	c->choice = g_strdup("");
	g_free(c->auto_name);
	c->auto_name = NULL;
	c->hint_done = 0;
	update_syntax_label(c);
	gtk_text_buffer_set_text(gtk_text_view_get_buffer(GTK_TEXT_VIEW(c->raw)), text, -1);
	gtk_notebook_set_current_page(GTK_NOTEBOOK(c->notebook), markdown ? 1 : 0);
	if (markdown) render_preview(c);
	c->quiet = 0;
}

void trecs_note_free(note_ctx *c) {
	if (c->timer) g_source_remove(c->timer);
	g_free(c->choice);
	g_free(c->auto_name);
}

static GtkWidget *add_row(GtkWidget *grid, int row, const char *label, GtkWidget *w, int top) {
	GtkWidget *l = gtk_label_new(label);
	gtk_widget_set_halign(l, GTK_ALIGN_START);
	gtk_widget_set_valign(l, top ? GTK_ALIGN_START : GTK_ALIGN_CENTER);
	gtk_grid_attach(GTK_GRID(grid), l, 0, row, 1, 1);
	gtk_widget_set_hexpand(w, TRUE);
	gtk_grid_attach(GTK_GRID(grid), w, 1, row, 1, 1);
	return w;
}

// Returns 1 = saved, 0 = cancelled, -1 = GTK could not start (no display).
int trecs_form(const char *app, const char *title, const char *text,
                      const char **sugg, int nsugg, int width, int height,
                      const unsigned char *icon, int iconlen,
                      char **o_app, char **o_title, char **o_tags, char **o_text) {
	if (!gtk_init_check(NULL, NULL)) return -1;

	GtkWidget *dlg = gtk_dialog_new_with_buttons("Trecs Notes", NULL, GTK_DIALOG_MODAL,
		"_Cancel", GTK_RESPONSE_CANCEL, "_Save", GTK_RESPONSE_ACCEPT, NULL);

	trecs_apply_header(dlg, "Trecs Notes", icon, iconlen);
	gtk_window_set_default_size(GTK_WINDOW(dlg), width > 0 ? width : 600, height > 0 ? height : 460);
	gtk_window_set_position(GTK_WINDOW(dlg), GTK_WIN_POS_CENTER);
	gtk_window_set_keep_above(GTK_WINDOW(dlg), TRUE);
	gtk_dialog_set_default_response(GTK_DIALOG(dlg), GTK_RESPONSE_ACCEPT);

	GtkWidget *grid = gtk_grid_new();
	gtk_grid_set_row_spacing(GTK_GRID(grid), 8);
	gtk_grid_set_column_spacing(GTK_GRID(grid), 10);
	gtk_container_set_border_width(GTK_CONTAINER(dlg), 14); // even space on every side, buttons included
	gtk_widget_set_margin_bottom(grid, 16);
	gtk_box_pack_start(GTK_BOX(gtk_dialog_get_content_area(GTK_DIALOG(dlg))), grid, TRUE, TRUE, 0);

	GtkWidget *e_app = add_row(grid, 0, "App", gtk_entry_new(), 0);
	gtk_entry_set_text(GTK_ENTRY(e_app), app);
	gtk_entry_set_activates_default(GTK_ENTRY(e_app), TRUE);

	GtkWidget *e_title = add_row(grid, 1, "Title", gtk_entry_new(), 0);
	gtk_entry_set_text(GTK_ENTRY(e_title), title);
	gtk_entry_set_activates_default(GTK_ENTRY(e_title), TRUE);

	GtkWidget *e_tags = add_row(grid, 2, "Tags", gtk_entry_new(), 0);
	gtk_entry_set_placeholder_text(GTK_ENTRY(e_tags), "space or comma separated");
	gtk_entry_set_activates_default(GTK_ENTRY(e_tags), TRUE);
	trecs_attach_tag_completion(e_tags, sugg, nsugg);

	note_ctx ctx;
	memset(&ctx, 0, sizeof ctx);
	ctx.dlg = dlg;
	add_row(grid, 3, "Note", trecs_note_area(&ctx, text), 1);

	gtk_widget_show_all(dlg);
	gtk_widget_grab_focus(e_tags);
	gtk_window_present(GTK_WINDOW(dlg));

	int resp = gtk_dialog_run(GTK_DIALOG(dlg));
	int saved = resp == GTK_RESPONSE_ACCEPT;
	if (saved) {
		GtkTextIter s, e;
		GtkTextBuffer *b = gtk_text_view_get_buffer(GTK_TEXT_VIEW(ctx.raw));
		gtk_text_buffer_get_bounds(b, &s, &e);
		*o_app = g_strdup(gtk_entry_get_text(GTK_ENTRY(e_app)));
		*o_title = g_strdup(gtk_entry_get_text(GTK_ENTRY(e_title)));
		*o_tags = g_strdup(gtk_entry_get_text(GTK_ENTRY(e_tags)));
		*o_text = gtk_text_buffer_get_text(b, &s, &e, FALSE);
	}
	trecs_note_free(&ctx);
	gtk_widget_destroy(dlg);
	while (gtk_events_pending()) gtk_main_iteration();
	return saved;
}
