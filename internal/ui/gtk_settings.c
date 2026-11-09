//go:build linux && !nogtk

#include "gtk_linux.h"
#include <stdlib.h>
#include <string.h>

enum { ACT_LOGIN = 1, ACT_LOGOUT = 2, ACT_SEND = 3, ACT_OPEN_FOLDER = 4, ACT_CHECK_HOTKEY = 5 };

typedef struct {
	GtkWidget *hotkey, *quick, *copyfb, *remote, *server, *email, *password, *login, *logout;
	GtkWidget *login_status, *unsynced, *send, *message, *remote_box;
} widgets;

static void set_message(widgets *w, const char *text, int ok) {
	char *esc = g_markup_escape_text(text, -1);
	char *m = g_strdup_printf("<span foreground=\"%s\">%s</span>", ok ? "#2e7d32" : "#c62828", esc);
	gtk_label_set_markup(GTK_LABEL(w->message), m);
	g_free(esc); g_free(m);
}

static void flush_events(void) { while (gtk_events_pending()) gtk_main_iteration(); }

// Calls into Go and applies its reply: "message \x1f login status \x1f unsynced \x1f ok".
static int act(widgets *w, int action, const char *a1, const char *a2, const char *a3, int show) {
	if (show) { set_message(w, "Working...", 1); flush_events(); }
	char *reply = goSettingsAction(action, (char *)a1, (char *)a2, (char *)a3);
	gchar **p = g_strsplit(reply, "\x1f", 4);
	int ok = 0;
	if (g_strv_length(p) == 4) {
		ok = atoi(p[3]);
		if (show || !ok) set_message(w, p[0], ok);
		if (p[1][0]) gtk_label_set_text(GTK_LABEL(w->login_status), p[1]);
		char *u = g_strdup_printf("%s note(s) not on the server yet", p[2]);
		gtk_label_set_text(GTK_LABEL(w->unsynced), u);
		g_free(u);
	}
	g_strfreev(p);
	free(reply);
	return ok;
}

static void on_login(GtkButton *b, gpointer d) {
	widgets *w = d;
	act(w, ACT_LOGIN, gtk_entry_get_text(GTK_ENTRY(w->server)), gtk_entry_get_text(GTK_ENTRY(w->email)),
	    gtk_entry_get_text(GTK_ENTRY(w->password)), 1);
}
static void on_logout(GtkButton *b, gpointer d) {
	widgets *w = d;
	if (act(w, ACT_LOGOUT, "", "", "", 1)) gtk_entry_set_text(GTK_ENTRY(w->password), ""); // the stored password is forgotten too
}
static void on_send(GtkButton *b, gpointer d) { widgets *w = d; act(w, ACT_SEND, "", "", "", 1); }
static void on_folder(GtkButton *b, gpointer d) { widgets *w = d; act(w, ACT_OPEN_FOLDER, "", "", "", 0); }

static void on_remote_toggled(GtkToggleButton *t, gpointer d) {
	widgets *w = d;
	gtk_widget_set_sensitive(w->remote_box, gtk_toggle_button_get_active(t));
}

static gboolean is_modifier_key(const char *n) {
	return n == NULL || g_str_has_prefix(n, "Control") || g_str_has_prefix(n, "Shift") || g_str_has_prefix(n, "Alt")
	    || g_str_has_prefix(n, "Super") || g_str_has_prefix(n, "Meta") || g_str_has_prefix(n, "Hyper")
	    || g_str_has_prefix(n, "ISO_") || g_str_has_prefix(n, "Caps") || g_str_has_prefix(n, "Num");
}

// The hotkey box records what is pressed instead of taking typed text.
static gboolean on_hotkey_key(GtkWidget *e, GdkEventKey *ev, gpointer d) {
	guint mods = ev->state & gtk_accelerator_get_default_mod_mask();
	guint want = GDK_CONTROL_MASK | GDK_MOD1_MASK | GDK_SHIFT_MASK | GDK_SUPER_MASK | GDK_MOD4_MASK;
	if (!(mods & want)) return FALSE; // a plain key (Tab, Esc...) keeps its normal job, so focus can still move on
	// the key itself, as if no modifier were held (Shift+1 is "1", not "!")
	guint kv = 0;
	gdk_keymap_translate_keyboard_state(gdk_keymap_get_for_display(gdk_display_get_default()),
	                                    ev->hardware_keycode, 0, ev->group, &kv, NULL, NULL, NULL);
	const char *n = gdk_keyval_name(kv);
	if (is_modifier_key(n)) return TRUE; // still holding the modifiers; wait for the real key
	GString *s = g_string_new("");
	if (mods & GDK_CONTROL_MASK) g_string_append(s, "Ctrl+");
	if (mods & GDK_MOD1_MASK) g_string_append(s, "Alt+");
	if (mods & GDK_SHIFT_MASK) g_string_append(s, "Shift+");
	if (mods & (GDK_SUPER_MASK | GDK_MOD4_MASK)) g_string_append(s, "Super+");
	if (strlen(n) == 1) g_string_append_c(s, g_ascii_toupper(n[0]));
	else if (strcmp(n, "space") == 0) g_string_append(s, "Space");
	else g_string_append(s, n);
	gtk_entry_set_text(GTK_ENTRY(e), s->str);
	g_string_free(s, TRUE);
	return TRUE;
}

// the quick-save box: the same, and Backspace or Delete empties it (= off)
static gboolean on_quick_key(GtkWidget *e, GdkEventKey *ev, gpointer d) {
	if ((ev->keyval == GDK_KEY_BackSpace || ev->keyval == GDK_KEY_Delete)
	    && !(ev->state & gtk_accelerator_get_default_mod_mask())) {
		gtk_entry_set_text(GTK_ENTRY(e), "");
		return TRUE;
	}
	return on_hotkey_key(e, ev, d);
}

static GtkWidget *label(const char *t) {
	GtkWidget *l = gtk_label_new(t);
	gtk_widget_set_halign(l, GTK_ALIGN_START);
	return l;
}

int trecs_settings(const settings_in *in, settings_out *out) {
	if (!gtk_init_check(NULL, NULL)) return -1;
	widgets w;
	memset(&w, 0, sizeof w);

	GtkWidget *dlg = gtk_dialog_new_with_buttons("Settings", NULL, GTK_DIALOG_MODAL,
		"_Cancel", GTK_RESPONSE_CANCEL, "_Save", GTK_RESPONSE_ACCEPT, NULL);
	trecs_apply_header(dlg, "Trecs Notes - Settings", in->icon, in->iconlen);
	gtk_window_set_position(GTK_WINDOW(dlg), GTK_WIN_POS_CENTER);
	gtk_window_set_keep_above(GTK_WINDOW(dlg), TRUE);
	gtk_container_set_border_width(GTK_CONTAINER(dlg), 14);

	GtkWidget *g = gtk_grid_new();
	gtk_grid_set_row_spacing(GTK_GRID(g), 8);
	gtk_grid_set_column_spacing(GTK_GRID(g), 10);
	gtk_widget_set_margin_bottom(g, 16);
	gtk_box_pack_start(GTK_BOX(gtk_dialog_get_content_area(GTK_DIALOG(dlg))), g, TRUE, TRUE, 0);
	int r = 0;

	// ---- capture
	gtk_grid_attach(GTK_GRID(g), label("Hotkey"), 0, r, 1, 1);
	w.hotkey = gtk_entry_new();
	gtk_widget_set_hexpand(w.hotkey, TRUE);
	gtk_entry_set_text(GTK_ENTRY(w.hotkey), in->hotkey);
	gtk_entry_set_placeholder_text(GTK_ENTRY(w.hotkey), "click here, then press the combination");
	gtk_editable_set_editable(GTK_EDITABLE(w.hotkey), FALSE);
	g_signal_connect(w.hotkey, "key-press-event", G_CALLBACK(on_hotkey_key), NULL);
	gtk_grid_attach(GTK_GRID(g), w.hotkey, 1, r++, 1, 1);

	gtk_grid_attach(GTK_GRID(g), label("Quick save"), 0, r, 1, 1);
	w.quick = gtk_entry_new();
	gtk_widget_set_hexpand(w.quick, TRUE);
	gtk_entry_set_text(GTK_ENTRY(w.quick), in->quick_hotkey);
	gtk_entry_set_placeholder_text(GTK_ENTRY(w.quick), "off - click here, press a combination (Backspace clears)");
	gtk_widget_set_tooltip_text(w.quick, "Stores the selected text at once, with no form and no tags. Does nothing when no text is selected.");
	gtk_editable_set_editable(GTK_EDITABLE(w.quick), FALSE);
	g_signal_connect(w.quick, "key-press-event", G_CALLBACK(on_quick_key), NULL);
	gtk_grid_attach(GTK_GRID(g), w.quick, 1, r++, 1, 1);

	gtk_grid_attach(GTK_GRID(g), label("Form size"), 0, r, 1, 1);
	GtkWidget *hb = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 6);
	GtkWidget *sw = gtk_spin_button_new_with_range(300, 2400, 10);
	GtkWidget *sh = gtk_spin_button_new_with_range(250, 1800, 10);
	gtk_spin_button_set_value(GTK_SPIN_BUTTON(sw), in->width);
	gtk_spin_button_set_value(GTK_SPIN_BUTTON(sh), in->height);
	gtk_box_pack_start(GTK_BOX(hb), sw, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(hb), gtk_label_new("x"), FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(hb), sh, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(hb), gtk_label_new("pixels, width x height"), FALSE, FALSE, 6);
	gtk_grid_attach(GTK_GRID(g), hb, 1, r++, 1, 1);

	w.copyfb = gtk_check_button_new_with_label("If an application does not report its selection, try copying it");
	gtk_toggle_button_set_active(GTK_TOGGLE_BUTTON(w.copyfb), in->copy_fallback);
	gtk_widget_set_tooltip_text(w.copyfb,
		"Sends Ctrl+C (Cmd+C on macOS) to the window and puts your clipboard back afterwards.\n"
		"Never done in a terminal, where Ctrl+C would interrupt the running program.");
	gtk_grid_attach(GTK_GRID(g), w.copyfb, 0, r++, 2, 1);

	gtk_grid_attach(GTK_GRID(g), gtk_separator_new(GTK_ORIENTATION_HORIZONTAL), 0, r++, 2, 1);

	// ---- remote sync
	w.remote = gtk_check_button_new_with_label("Also save notes to a trecs server");
	gtk_toggle_button_set_active(GTK_TOGGLE_BUTTON(w.remote), in->remote);
	gtk_grid_attach(GTK_GRID(g), w.remote, 0, r++, 2, 1);

	w.remote_box = gtk_grid_new();
	gtk_grid_set_row_spacing(GTK_GRID(w.remote_box), 8);
	gtk_grid_set_column_spacing(GTK_GRID(w.remote_box), 10);
	gtk_widget_set_margin_start(w.remote_box, 24);
	gtk_widget_set_hexpand(w.remote_box, TRUE);
	gtk_grid_attach(GTK_GRID(g), w.remote_box, 0, r++, 2, 1);
	int q = 0;

	gtk_grid_attach(GTK_GRID(w.remote_box), label("Server"), 0, q, 1, 1);
	w.server = gtk_entry_new();
	gtk_widget_set_hexpand(w.server, TRUE);
	gtk_entry_set_text(GTK_ENTRY(w.server), in->server);
	gtk_grid_attach(GTK_GRID(w.remote_box), w.server, 1, q++, 1, 1);

	gtk_grid_attach(GTK_GRID(w.remote_box), label("Email"), 0, q, 1, 1);
	w.email = gtk_entry_new();
	gtk_widget_set_hexpand(w.email, TRUE);
	gtk_entry_set_text(GTK_ENTRY(w.email), in->email);
	gtk_grid_attach(GTK_GRID(w.remote_box), w.email, 1, q++, 1, 1);

	gtk_grid_attach(GTK_GRID(w.remote_box), label("Password"), 0, q, 1, 1);
	w.password = gtk_entry_new();
	gtk_entry_set_visibility(GTK_ENTRY(w.password), FALSE);
	gtk_entry_set_text(GTK_ENTRY(w.password), in->password);
	gtk_widget_set_hexpand(w.password, TRUE);
	gtk_grid_attach(GTK_GRID(w.remote_box), w.password, 1, q++, 1, 1);

	GtkWidget *lb = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
	w.login = gtk_button_new_with_label("Log in");
	w.logout = gtk_button_new_with_label("Log out");
	w.login_status = label(in->login_status);
	gtk_box_pack_start(GTK_BOX(lb), w.login, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(lb), w.logout, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(lb), w.login_status, FALSE, FALSE, 6);
	gtk_grid_attach(GTK_GRID(w.remote_box), lb, 1, q++, 1, 1);

	GtkWidget *ub = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
	char *u = g_strdup_printf("%d note(s) not on the server yet", in->unsynced);
	w.unsynced = label(u);
	g_free(u);
	w.send = gtk_button_new_with_label("Send them now");
	gtk_box_pack_start(GTK_BOX(ub), w.send, FALSE, FALSE, 0);
	gtk_box_pack_start(GTK_BOX(ub), w.unsynced, FALSE, FALSE, 6);
	gtk_grid_attach(GTK_GRID(w.remote_box), ub, 1, q++, 1, 1);

	g_signal_connect(w.remote, "toggled", G_CALLBACK(on_remote_toggled), &w);
	g_signal_connect(w.login, "clicked", G_CALLBACK(on_login), &w);
	g_signal_connect(w.logout, "clicked", G_CALLBACK(on_logout), &w);
	g_signal_connect(w.send, "clicked", G_CALLBACK(on_send), &w);
	gtk_widget_set_sensitive(w.remote_box, in->remote);

	gtk_grid_attach(GTK_GRID(g), gtk_separator_new(GTK_ORIENTATION_HORIZONTAL), 0, r++, 2, 1);

	// ---- database
	gtk_grid_attach(GTK_GRID(g), label("Database"), 0, r, 1, 1);
	GtkWidget *db = gtk_box_new(GTK_ORIENTATION_HORIZONTAL, 8);
	GtkWidget *path = label(in->db_path);
	gtk_label_set_selectable(GTK_LABEL(path), TRUE);
	gtk_label_set_ellipsize(GTK_LABEL(path), PANGO_ELLIPSIZE_MIDDLE);
	gtk_widget_set_hexpand(path, TRUE);
	GtkWidget *open = gtk_button_new_with_label("Open folder");
	g_signal_connect(open, "clicked", G_CALLBACK(on_folder), &w);
	gtk_box_pack_start(GTK_BOX(db), path, TRUE, TRUE, 0);
	gtk_box_pack_start(GTK_BOX(db), open, FALSE, FALSE, 0);
	gtk_grid_attach(GTK_GRID(g), db, 1, r++, 1, 1);

	w.message = gtk_label_new("");
	gtk_label_set_line_wrap(GTK_LABEL(w.message), TRUE);
	gtk_widget_set_halign(w.message, GTK_ALIGN_START);
	gtk_grid_attach(GTK_GRID(g), w.message, 0, r++, 2, 1);

	gtk_window_set_default_size(GTK_WINDOW(dlg), 560, -1);
	gtk_widget_show_all(dlg);
	gtk_window_present(GTK_WINDOW(dlg));

	int saved = 0;
	for (;;) {
		int resp = gtk_dialog_run(GTK_DIALOG(dlg));
		if (resp != GTK_RESPONSE_ACCEPT) break;
		// a new hotkey is checked (and tried) before the window closes, so a
		// combination another program owns is refused here, with a reason
		const char *hk = gtk_entry_get_text(GTK_ENTRY(w.hotkey));
		const char *qk = gtk_entry_get_text(GTK_ENTRY(w.quick));
		if (strcmp(hk, in->hotkey) != 0) {
			if (!act(&w, ACT_CHECK_HOTKEY, hk, qk, "", 0)) continue;
		}
		if (qk[0] && strcmp(qk, in->quick_hotkey) != 0) {
			if (!act(&w, ACT_CHECK_HOTKEY, qk, hk, "", 0)) continue;
		}
		saved = 1;
		break;
	}
	if (saved) {
		out->hotkey = g_strdup(gtk_entry_get_text(GTK_ENTRY(w.hotkey)));
		out->quick_hotkey = g_strdup(gtk_entry_get_text(GTK_ENTRY(w.quick)));
		out->width = gtk_spin_button_get_value_as_int(GTK_SPIN_BUTTON(sw));
		out->height = gtk_spin_button_get_value_as_int(GTK_SPIN_BUTTON(sh));
		out->remote = gtk_toggle_button_get_active(GTK_TOGGLE_BUTTON(w.remote));
		out->copy_fallback = gtk_toggle_button_get_active(GTK_TOGGLE_BUTTON(w.copyfb));
		out->server = g_strdup(gtk_entry_get_text(GTK_ENTRY(w.server)));
		out->email = g_strdup(gtk_entry_get_text(GTK_ENTRY(w.email)));
		out->password = g_strdup(gtk_entry_get_text(GTK_ENTRY(w.password)));
	}
	gtk_widget_destroy(dlg);
	flush_events();
	return saved;
}
