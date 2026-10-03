package mates

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestManifestAsksForCrewsPermissionsAndNoWebhook(t *testing.T) {
	data, err := json.Marshal(NewManifest("tester", "thatsnotmynameio", "http://127.0.0.1:4242/created"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	keys := slices.Sorted(maps.Keys(got))
	want := []string{"default_permissions", "description", "name", "public", "redirect_url", "url"}
	if !slices.Equal(keys, want) {
		t.Errorf("manifest keys = %q, want %q (no hook_attributes, no default_events)", keys, want)
	}
	for key, want := range map[string]any{
		"name":         "crew-tester",
		"url":          "https://github.com/thatsnotmynameio/crew",
		"redirect_url": "http://127.0.0.1:4242/created",
		"public":       false,
	} {
		if got[key] != want {
			t.Errorf("manifest %s = %v, want %v", key, got[key], want)
		}
	}
	desc, _ := got["description"].(string)
	if !strings.Contains(desc, "tester") || !strings.Contains(desc, "thatsnotmynameio") {
		t.Errorf("description %q names neither the mate nor the owner", desc)
	}
	wantPerms := map[string]any{
		"issues": "write", "pull_requests": "write", "contents": "read", "checks": "read",
		"statuses": "read", "actions": "read", "metadata": "read",
	}
	perms, _ := got["default_permissions"].(map[string]any)
	if !maps.Equal(perms, wantPerms) {
		t.Errorf("default_permissions = %v, want %v", perms, wantPerms)
	}
	if perms["contents"] == "write" || perms["workflows"] != nil {
		t.Errorf("default_permissions %v can write code", perms)
	}
}
