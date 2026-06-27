cat << 'INNER_EOF' > patch.diff
--- video-converter-master/internal/db/tracker.go
+++ video-converter-master/internal/db/tracker.go
@@ -620,10 +620,13 @@
		ORDER BY created_at DESC
	`

+	args := []any{startTime, endTime}
+
	if limit > 0 {
-		query += fmt.Sprintf(" LIMIT %d", limit)
+		query += " LIMIT ?"
+		args = append(args, limit)
	}

-	rows, err := t.db.Query(query, startTime, endTime)
+	rows, err := t.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query job history: %w", err)
INNER_EOF
patch -p0 < patch.diff
