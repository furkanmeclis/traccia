package attachments_test

import "testing"

func TestAttachmentMigrationRoundtrip(t *testing.T) {
	pool := isolatedDatabase(t)
	var count int
	if e := pool.QueryRow(t.Context(), "SELECT count(*) FROM attachment_uploads").Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 0 {
		t.Fatal("fixture is not isolated")
	}
}
