#ifndef TRECS_GTK_LINUX_H
#define TRECS_GTK_LINUX_H
#include <gtk/gtk.h>

// The note area (gtk_form.c): a Raw tab with syntax colours and a Markdown
// preview, fed by Go (goRender). Used by the capture form and the browse window.
typedef struct {
	GtkWidget *raw, *preview, *notebook, *dlg;
	GtkWidget *syn_button, *syn_popover, *syn_search, *syn_list;
	guint timer;
	int syntaxes_loaded, hint_done;
	int quiet;         // set while a note is being loaded: no focus grabs
	char *choice;      // "" = auto, "none", or a language name
	char *auto_name;   // what auto resolved to last time
} note_ctx;

GtkWidget *trecs_note_area(note_ctx *c, const char *text);
void trecs_note_load(note_ctx *c, const char *text, int markdown);
void trecs_note_free(note_ctx *c);
char *trecs_buffer_text(GtkTextBuffer *b);
GtkWidget *trecs_scrolled(GtkWidget *child);
void trecs_attach_tag_completion(GtkWidget *entry, const char **sugg, int nsugg);

void trecs_apply_header(GtkWidget *dlg, const char *title, const unsigned char *icon, int iconlen);

int trecs_form(const char *app, const char *title, const char *text,
               const char **sugg, int nsugg, int width, int height,
               const unsigned char *icon, int iconlen,
               char **o_app, char **o_title, char **o_tags, char **o_text);

typedef struct {
	const char *hotkey, *quick_hotkey;
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
	char *hotkey, *quick_hotkey;
	int width, height;
	int remote, copy_fallback;
	char *server, *email, *password;
} settings_out;

// 1 = saved, 0 = cancelled, -1 = GTK could not start.
int trecs_settings(const settings_in *in, settings_out *out);

// Implemented in Go (gtk_render_linux.go): text for the note area, as styled
// runs or colour spans (see internal/render). Returns a malloc'd string.
extern char *goRender(int op, char *a, char *b);

// Implemented in Go (gtk_settings_linux.go): the settings window's buttons
// call it. Returns a malloc'd "message \x1f login status \x1f unsynced \x1f
// ok(0/1)".
extern char *goSettingsAction(int action, char *a1, char *a2, char *a3);

typedef struct {
	const unsigned char *icon;
	int iconlen;
	int width, height;
} browse_in;

// The browse window. Returns 0 when it was closed, -1 if GTK could not start.
int trecs_browse(const browse_in *in);

// Called from any thread (Go's search goroutine): hands search results to the
// window. kind 0: rows "id \x1f mark \x1f date \x1f app \x1f snippet" joined by
// \x1e; kind 1: the search is over, payload "capped \x1f problem".
void trecs_browse_deliver(int gen, const char *payload, int kind);

// Implemented in Go (gtk_browse_linux.go); every answer is malloc'd.
extern char *goBrowseAction(int op, char *a1, char *a2, char *a3, char *a4, char *a5);

#endif
