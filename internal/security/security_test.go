package security

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil { t.Fatal(err) }
	if !VerifyPassword(hash, "correct horse battery staple") { t.Fatal("valid password rejected") }
	if VerifyPassword(hash, "incorrect password") { t.Fatal("invalid password accepted") }
}

func TestShortPasswordRejected(t *testing.T) {
	if _, err := HashPassword("short"); err == nil { t.Fatal("expected short password error") }
}

func TestTokensAreRandom(t *testing.T) {
	a, _ := RandomToken(32); b, _ := RandomToken(32)
	if a == b || len(a) < 40 { t.Fatal("unexpected token output") }
	if TokenHash(a) == a { t.Fatal("token hash must not expose token") }
}
