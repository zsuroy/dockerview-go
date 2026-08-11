package audit

import (
	"net/http"
	"testing"
)

func TestHashActorStable(t *testing.T) {
	a := HashActor("secret")
	b := HashActor("secret")
	if a != b {
		t.Fatalf("hash not stable: %s vs %s", a, b)
	}
	c := HashActor("different")
	if a == c {
		t.Fatal("hash should differ for different tokens")
	}
	if len(a) != len("tok_")+12 {
		t.Fatalf("hash wrong length: %s", a)
	}
}

func TestDeriveSource(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (X11; Linux) AppleWebKit/537.36": SourceWeb,
		"DockerviewMobile/1.0 (iOS)":                  SourceMobile,
		"Expo/57 (Android)":                           SourceMobile,
		"curl/8.5.0":                                  SourceAPI,
		"Wget/1.21":                                   SourceAPI,
		"":                                            SourceUnknown,
		"CustomClient/1.0":                            SourceAPI,
	}
	for ua, want := range cases {
		if got := DeriveSource(ua); got != want {
			t.Errorf("UA %q: got %s want %s", ua, got, want)
		}
	}
}

func TestActorFromRequest(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0")
	r.RemoteAddr = "10.0.0.5:12345"
	actor, kind, source, ip, ua := ActorFromRequest(r, "secret")
	if kind != ActorKindToken {
		t.Fatalf("kind=%s", kind)
	}
	if actor != HashActor("secret") {
		t.Fatalf("actor mismatch")
	}
	if source != SourceWeb {
		t.Fatalf("source=%s", source)
	}
	if ip != "10.0.0.5" {
		t.Fatalf("ip=%s", ip)
	}
	if ua == "" {
		t.Fatal("ua empty")
	}

	// anonymous
	r2, _ := http.NewRequest("GET", "/", nil)
	_, kind2, _, _, _ := ActorFromRequest(r2, "")
	if kind2 != ActorKindAnon {
		t.Fatalf("anonymous kind=%s", kind2)
	}

	// X-Forwarded-For
	r3, _ := http.NewRequest("GET", "/", nil)
	r3.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	r3.RemoteAddr = "127.0.0.1:1"
	_, _, _, ip3, _ := ActorFromRequest(r3, "")
	if ip3 != "1.2.3.4" {
		t.Fatalf("xff ip=%s", ip3)
	}
}

func TestCLIEvent(t *testing.T) {
	e := CLIEvent(ActionStart, "id1", "c1", ResultSuccess, "", nil)
	if e.Actor != "cli" || e.ActorKind != ActorKindCLI || e.Source != SourceCLI {
		t.Fatalf("CLI event wrong: %+v", e)
	}
	if e.RequestID == "" {
		t.Fatal("request_id empty")
	}
}
