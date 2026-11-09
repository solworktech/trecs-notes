#ifndef TRECS_GTK_LINUX_H
#define TRECS_GTK_LINUX_H
#include <gtk/gtk.h>

void trecs_apply_header(GtkWidget *dlg, const char *title, const unsigned char *icon, int iconlen);

int trecs_form(const char *app, const char *title, const char *text,
               const char **sugg, int nsugg, int width, int height,
               const unsigned char *icon, int iconlen,
               char **o_app, char **o_title, char **o_tags, char **o_text);

typedef struct {
	const char *hotkey;
	int width, height;
	int remote, copy_fallback;
	const char *server, *email, *password;
	const char *login_status;
	int unsynced;
	const char *db_path;
	const unsigned char *icon;
	int iconlen;
} settings_in;

typedef struct {
	char *hotkey;
	int width, height;
	int remote, copy_fallback;
	char *server, *email, *password;
} settings_out;

// 1 = saved, 0 = cancelled, -1 = GTK could not start.
int trecs_settings(const settings_in *in, settings_out *out);

// Implemented in Go (gtk_linux.go): the settings window's buttons call it.
// Returns a malloc'd "message \x1f login status \x1f unsynced \x1f ok(0/1)".
// Implemented in Go (gtk_render_linux.go): text for the note area, as styled
// runs or colour spans (see internal/render). Returns a malloc'd string.
extern char *goRender(int op, char *a, char *b);

extern char *goSettingsAction(int action, char *a1, char *a2, char *a3);
#endif
