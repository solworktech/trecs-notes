//go:build linux && !nogtk

#include "gtk_linux.h"

// The "this note holds a secret" question. 1 = mask and save, 2 = save as it
// is, 0 = cancel (also Esc and closing the window), -1 = GTK could not start.
int trecs_secrets(const char *msg) {
	if (!gtk_init_check(NULL, NULL)) return -1;
	GtkWidget *dlg = gtk_message_dialog_new(NULL, GTK_DIALOG_MODAL, GTK_MESSAGE_WARNING,
	                                        GTK_BUTTONS_NONE, "%s", "Save this note with its secrets?");
	gtk_message_dialog_format_secondary_text(GTK_MESSAGE_DIALOG(dlg), "%s", msg);
	gtk_window_set_title(GTK_WINDOW(dlg), "Trecs Notes");
	gtk_window_set_position(GTK_WINDOW(dlg), GTK_WIN_POS_CENTER);
	gtk_window_set_keep_above(GTK_WINDOW(dlg), TRUE);
	gtk_dialog_add_button(GTK_DIALOG(dlg), "Cancel", GTK_RESPONSE_CANCEL);
	gtk_dialog_add_button(GTK_DIALOG(dlg), "Save as is", GTK_RESPONSE_NO);
	gtk_dialog_add_button(GTK_DIALOG(dlg), "Mask and save", GTK_RESPONSE_YES);
	gtk_dialog_set_default_response(GTK_DIALOG(dlg), GTK_RESPONSE_YES);
	gint r = gtk_dialog_run(GTK_DIALOG(dlg));
	gtk_widget_destroy(dlg);
	while (gtk_events_pending()) gtk_main_iteration();
	if (r == GTK_RESPONSE_YES) return 1;
	if (r == GTK_RESPONSE_NO) return 2;
	return 0;
}
