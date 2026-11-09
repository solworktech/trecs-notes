//go:build linux && !nogtk

#include "gtk_linux.h"
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

// A client-side header bar instead of the window manager's title bar: the
// WM decides title alignment (xfwm4 left-aligns), a GtkHeaderBar always
// centres it, and it can carry the icon on every desktop.
void trecs_apply_header(GtkWidget *dlg, const char *title, const unsigned char *icon, int iconlen) {
	GtkWidget *hb = gtk_header_bar_new();
	gtk_header_bar_set_title(GTK_HEADER_BAR(hb), title);
	gtk_header_bar_set_show_close_button(GTK_HEADER_BAR(hb), TRUE);
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
	if (nsugg > 0) {
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

	GtkWidget *tv = gtk_text_view_new();
	gtk_text_view_set_wrap_mode(GTK_TEXT_VIEW(tv), GTK_WRAP_WORD_CHAR);
	gtk_text_view_set_monospace(GTK_TEXT_VIEW(tv), TRUE);
	gtk_text_buffer_set_text(gtk_text_view_get_buffer(GTK_TEXT_VIEW(tv)), text, -1);
	GtkWidget *sw = gtk_scrolled_window_new(NULL, NULL);
	gtk_scrolled_window_set_shadow_type(GTK_SCROLLED_WINDOW(sw), GTK_SHADOW_IN);
	gtk_container_add(GTK_CONTAINER(sw), tv);
	gtk_widget_set_vexpand(sw, TRUE);
	add_row(grid, 3, "Note", sw, 1);

	gtk_widget_show_all(dlg);
	gtk_widget_grab_focus(e_tags);
	gtk_window_present(GTK_WINDOW(dlg));

	int resp = gtk_dialog_run(GTK_DIALOG(dlg));
	int saved = resp == GTK_RESPONSE_ACCEPT;
	if (saved) {
		GtkTextIter s, e;
		GtkTextBuffer *b = gtk_text_view_get_buffer(GTK_TEXT_VIEW(tv));
		gtk_text_buffer_get_bounds(b, &s, &e);
		*o_app = g_strdup(gtk_entry_get_text(GTK_ENTRY(e_app)));
		*o_title = g_strdup(gtk_entry_get_text(GTK_ENTRY(e_title)));
		*o_tags = g_strdup(gtk_entry_get_text(GTK_ENTRY(e_tags)));
		*o_text = gtk_text_buffer_get_text(b, &s, &e, FALSE);
	}
	gtk_widget_destroy(dlg);
	while (gtk_events_pending()) gtk_main_iteration();
	return saved;
}
