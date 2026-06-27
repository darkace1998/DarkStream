cat << 'INNER_EOF' > patch.diff
--- video-converter-master/internal/db/tracker.go
+++ video-converter-master/internal/db/tracker.go
@@ -552,10 +552,13 @@
		ORDER BY created_at DESC
	`

+	args := []any{status}
+
	if limit > 0 {
-		query += fmt.Sprintf(" LIMIT %d", limit)
+		query += " LIMIT ?"
+		args = append(args, limit)
	}

-	rows, err := t.db.Query(query, status)
+	rows, err := t.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query jobs by status: %w", err)
INNER_EOF
patch -p0 < patch.diff
