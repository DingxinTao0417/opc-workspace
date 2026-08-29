package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testPNGAvatar = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}

func replaceWorkspaceAvatarForTest(t *testing.T, router http.Handler, expectedVersion int, currentRef *string, content []byte) (settingsResponse, string) {
	t.Helper()
	currentJSON := "null"
	if currentRef != nil {
		encoded, _ := json.Marshal(*currentRef)
		currentJSON = string(encoded)
	}
	manifest := `{"operation":"replace","updates":[{"key":"workspace","expected_version":` +
		jsonNumber(expectedVersion) + `,"value":{"display_name":"Avatar Workspace","avatar_ref":` + currentJSON + `}}]}`
	response := performMultipartPartsRequest(router, "/api/v1/settings/avatar", []multipartTestPart{
		{field: "manifest", content: []byte(manifest)},
		{field: "file", filename: "avatar.png", content: content},
	}, nil, false)
	if response.Code != http.StatusOK {
		t.Fatalf("replace workspace avatar = %d: %s", response.Code, response.Body.String())
	}
	settings := decodeSettingsResponseForTest(t, response.Body.Bytes())
	var workspace workspaceSettingValue
	if err := json.Unmarshal(settingItemForTest(t, settings, "workspace").Value, &workspace); err != nil || workspace.AvatarRef == nil {
		t.Fatalf("decode workspace avatar response: value=%s err=%v", settingItemForTest(t, settings, "workspace").Value, err)
	}
	return settings, *workspace.AvatarRef
}

func jsonNumber(value int) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestWorkspaceAvatarReplaceReadAndRemoveLifecycle(t *testing.T) {
	router, store, artifactDir, _ := newBackupTestAPI(t)
	settings, firstRef := replaceWorkspaceAvatarForTest(t, router, 0, nil, testPNGAvatar)
	if item := settingItemForTest(t, settings, "workspace"); item.Version != 1 || !item.Stored {
		t.Fatalf("workspace after avatar create = %#v", item)
	}
	if !strings.HasPrefix(firstRef, "avatars/") || !strings.HasSuffix(firstRef, ".png") {
		t.Fatalf("workspace avatar ref = %q", firstRef)
	}
	if content := performRequest(router, http.MethodGet, "/api/v1/settings/avatar/content", nil, nil); content.Code != http.StatusOK ||
		content.Header().Get("Content-Type") != "image/png" || string(content.Body.Bytes()) != string(testPNGAvatar) {
		t.Fatalf("avatar content = %d headers=%v body=%x", content.Code, content.Header(), content.Body.Bytes())
	}

	secondBytes := append(append([]byte(nil), testPNGAvatar...), 1)
	_, secondRef := replaceWorkspaceAvatarForTest(t, router, 1, &firstRef, secondBytes)
	if secondRef == firstRef {
		t.Fatal("avatar replacement reused controlled identity")
	}
	if _, err := os.Stat(filepath.Join(artifactDir, filepath.FromSlash(firstRef))); !os.IsNotExist(err) {
		t.Fatalf("replaced avatar file was not removed: %v", err)
	}
	var retired, tombstones int64
	if err := store.DB.Table("workspace_avatars").Where("relative_path = ? AND deleted_at IS NOT NULL", firstRef).Count(&retired).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Table("workspace_avatar_deletion_tombstones").Where("relative_path = ?", firstRef).Count(&tombstones).Error; err != nil {
		t.Fatal(err)
	}
	if retired != 1 || tombstones != 1 {
		t.Fatalf("replacement history retired=%d tombstones=%d", retired, tombstones)
	}

	removeManifest := `{"operation":"remove","updates":[{"key":"workspace","expected_version":2,"value":{"display_name":"Avatar Workspace","avatar_ref":"` + secondRef + `"}}]}`
	removed := performMultipartPartsRequest(router, "/api/v1/settings/avatar", []multipartTestPart{{field: "manifest", content: []byte(removeManifest)}}, nil, false)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove workspace avatar = %d: %s", removed.Code, removed.Body.String())
	}
	var workspace workspaceSettingValue
	response := decodeSettingsResponseForTest(t, removed.Body.Bytes())
	if err := json.Unmarshal(settingItemForTest(t, response, "workspace").Value, &workspace); err != nil || workspace.AvatarRef != nil {
		t.Fatalf("workspace avatar was not cleared: %#v err=%v", workspace, err)
	}
	if content := performRequest(router, http.MethodGet, "/api/v1/settings/avatar/content", nil, nil); content.Code != http.StatusNotFound {
		t.Fatalf("removed avatar content = %d: %s", content.Code, content.Body.String())
	}
}

func TestWorkspaceAvatarRejectsInvalidFilesAndGenericReferenceWrites(t *testing.T) {
	router, _, _, _ := newBackupTestAPI(t)
	manifest := `{"operation":"replace","updates":[{"key":"workspace","expected_version":0,"value":{"display_name":"Workspace","avatar_ref":null}}]}`
	invalid := performMultipartPartsRequest(router, "/api/v1/settings/avatar", []multipartTestPart{
		{field: "manifest", content: []byte(manifest)},
		{field: "file", filename: "avatar.txt", content: []byte("not an image")},
	}, nil, false)
	if invalid.Code != http.StatusUnprocessableEntity || responseErrorCode(t, invalid.Body.Bytes()) != "VALIDATION_ERROR" {
		t.Fatalf("invalid avatar = %d: %s", invalid.Code, invalid.Body.String())
	}
	generic := performRequest(router, http.MethodPatch, "/api/v1/settings", []byte(`{"updates":[{"key":"workspace","expected_version":0,"value":{"display_name":"Workspace","avatar_ref":"avatars/018f0000-0000-4000-8000-000000002799.png"}}]}`), nil)
	if generic.Code != http.StatusUnprocessableEntity || responseErrorCode(t, generic.Body.Bytes()) != "AVATAR_WRITE_REQUIRES_MULTIPART" {
		t.Fatalf("generic avatar write = %d: %s", generic.Code, generic.Body.String())
	}
}

func TestWorkspaceAvatarStartupReconcileMarksMismatchAndQuarantinesOrphans(t *testing.T) {
	router, databaseStore, artifactDir, _ := newBackupTestAPI(t)
	_, avatarRef := replaceWorkspaceAvatarForTest(t, router, 0, nil, testPNGAvatar)
	avatarPath := filepath.Join(artifactDir, filepath.FromSlash(avatarRef))
	if err := os.WriteFile(avatarPath, []byte("tampered-avatar!"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &artifactStore{
		root:          artifactDir,
		avatarsDir:    filepath.Join(artifactDir, "avatars"),
		quarantineDir: filepath.Join(artifactDir, ".quarantine"),
	}
	if err := store.reconcileWorkspaceAvatars(databaseStore.DB); err != nil {
		t.Fatalf("reconcile mismatched avatar: %v", err)
	}
	var status string
	if err := databaseStore.SQL.QueryRow("SELECT integrity_status FROM workspace_avatars WHERE relative_path = ?", avatarRef).Scan(&status); err != nil || status != "mismatch" {
		t.Fatalf("avatar integrity status=%q err=%v", status, err)
	}
	orphanName := "018f0000-0000-4000-8000-000000002798.png"
	orphanPath := filepath.Join(artifactDir, "avatars", orphanName)
	if err := os.WriteFile(orphanPath, testPNGAvatar, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.reconcileWorkspaceAvatars(databaseStore.DB); err != nil {
		t.Fatalf("reconcile orphan avatar: %v", err)
	}
	if _, err := os.Lstat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("orphan avatar remained live: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(artifactDir, ".quarantine"))
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), orphanName+"-") {
		t.Fatalf("avatar quarantine entries=%v err=%v", entries, err)
	}
}
