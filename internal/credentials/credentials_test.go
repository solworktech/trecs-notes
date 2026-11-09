package credentials

import "testing"

func TestRoundTripAndDelete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "") // no keyring: exercises the file fallback
	in := Login{Server: "https://x.example", Email: "a@b.c", Password: "pw"}
	if _, err := Save(in); err != nil {
		t.Fatal(err)
	}
	got, ok := Load()
	if !ok || got != in {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	Delete()
	if _, ok := Load(); ok {
		t.Fatal("still there after Delete")
	}
}
