//go:build ignore

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/identity"
)

func makeTestJWT(claims map[string]interface{}) string {
	header := map[string]interface{}{"alg": "none", "typ": "JWT"}
	h, _ := json.Marshal(header)
	p, _ := json.Marshal(claims)
	return base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p) + ".sig"
}

func main() {
	dir := "/tmp/caam-debug"
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "auth.json")
	payload := map[string]interface{}{"id_token": makeTestJWT(map[string]interface{}{"email": "work@gmail.com"})}
	b, _ := json.Marshal(payload)
	_ = os.WriteFile(path, b, 0o600)

	id, err := identity.ExtractFromCodexAuth(path)
	fmt.Printf("err=%v\n", err)
	fmt.Printf("id=%+v\n", id)
}
