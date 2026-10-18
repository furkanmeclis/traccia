package storage

import "fmt"

// AppLogoObjectKey builds app/branding/logo.{ext} for system letterhead.
func AppLogoObjectKey(ext string) string {
	return fmt.Sprintf("app/branding/logo.%s", trimExt(ext))
}

// ExportObjectKey builds exports/{jobUUID}/export.{ext}.
func ExportObjectKey(jobUUID, ext string) string {
	return fmt.Sprintf("exports/%s/export.%s", jobUUID, trimExt(ext))
}

// ImportSourceObjectKey builds imports/{jobUUID}/source.{ext}.
func ImportSourceObjectKey(jobUUID, ext string) string {
	return fmt.Sprintf("imports/%s/source.%s", jobUUID, trimExt(ext))
}

func trimExt(ext string) string {
	for len(ext) > 0 && ext[0] == '.' {
		ext = ext[1:]
	}
	return ext
}

// AttachmentUploadPrefix hides product bytes from the global storage explorer.
func AttachmentUploadPrefix(workspaceUUID, uploadUUID string) string {
	return SystemPrefix + "attachments/" + workspaceUUID + "/" + uploadUUID + "/"
}

// AttachmentChunkKey includes content identity so canceled old writes cannot
// overwrite a different committed retry at the same offset.
func AttachmentChunkKey(workspaceUUID, uploadUUID string, offset int64, digest string) string {
	return fmt.Sprintf("%sstaging/chunks/%d/%s", AttachmentUploadPrefix(workspaceUUID, uploadUUID), offset, digest)
}
func AttachmentManifestKey(workspaceUUID, uploadUUID, digest string) string {
	return AttachmentUploadPrefix(workspaceUUID, uploadUUID) + "final/" + digest + ".json"
}

func AttachmentStagingPrefix(workspaceUUID, uploadUUID string) string {
	return AttachmentUploadPrefix(workspaceUUID, uploadUUID) + "staging/"
}
func AttachmentFinalChunkKey(workspaceUUID, uploadUUID string, offset int64, digest string) string {
	return fmt.Sprintf("%sfinal/chunks/%d/%s", AttachmentUploadPrefix(workspaceUUID, uploadUUID), offset, digest)
}
