package store

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 50
	maxSearchBodyRunes = 200_000
	snippetPadRunes    = 40
	ftsMinRunes        = 3
)

// ThreadSearchHit is one conversation that matched a keyword query.
type ThreadSearchHit struct {
	ThreadID string
	Title    string
	Snippet  string
}

type searchDoc struct {
	ID       int64  `gorm:"primaryKey"`
	ThreadID string `gorm:"uniqueIndex;size:64"`
	Title    string
	Body     string
}

func (searchDoc) TableName() string { return "search_docs" }

func (s *Store) ensureSearch() error {
	if err := s.db.AutoMigrate(&searchDoc{}, &SearchChunk{}); err != nil {
		return fmt.Errorf("store: search migrate: %w", err)
	}
	if err := s.ensureThreadFTS(); err != nil {
		return err
	}
	var docs, threads int64
	if err := s.db.Model(&searchDoc{}).Count(&docs).Error; err != nil {
		return fmt.Errorf("store: count search docs: %w", err)
	}
	if err := s.db.Model(&Thread{}).Count(&threads).Error; err != nil {
		return fmt.Errorf("store: count threads: %w", err)
	}
	if docs == 0 && threads > 0 {
		return s.rebuildAllSearch()
	}
	return nil
}

func (s *Store) ensureThreadFTS() error {
	var n int
	if err := s.db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", "thread_fts").Scan(&n).Error; err != nil {
		return fmt.Errorf("store: lookup thread_fts: %w", err)
	}
	if n > 0 {
		return nil
	}
	stmts := []string{
		`CREATE VIRTUAL TABLE thread_fts USING fts5(
			title,
			body,
			content='search_docs',
			content_rowid='id',
			tokenize='trigram'
		)`,
		`CREATE TRIGGER search_docs_ai AFTER INSERT ON search_docs BEGIN
			INSERT INTO thread_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
		END`,
		`CREATE TRIGGER search_docs_ad AFTER DELETE ON search_docs BEGIN
			INSERT INTO thread_fts(thread_fts, rowid, title, body) VALUES('delete', old.id, old.title, old.body);
		END`,
		`CREATE TRIGGER search_docs_au AFTER UPDATE ON search_docs BEGIN
			INSERT INTO thread_fts(thread_fts, rowid, title, body) VALUES('delete', old.id, old.title, old.body);
			INSERT INTO thread_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
		END`,
		// content= does not copy existing search_docs rows. An upgrade that
		// already has keyword docs would otherwise MATCH nothing until the
		// next title edit, and LIKE would be the only thing keeping ⌘K alive.
		`INSERT INTO thread_fts(thread_fts) VALUES('rebuild')`,
	}
	for _, q := range stmts {
		if err := s.db.Exec(q).Error; err != nil {
			return fmt.Errorf("store: create thread_fts: %w", err)
		}
	}
	return nil
}

func (s *Store) rebuildAllSearch() error {
	var threads []Thread
	if err := s.db.Find(&threads).Error; err != nil {
		return fmt.Errorf("store: list threads for search rebuild: %w", err)
	}
	for i := range threads {
		if err := s.IndexThread(threads[i].ID); err != nil {
			return err
		}
	}
	return nil
}

// IndexThread rebuilds the keyword document for one conversation from its
// title, standing goal and user/assistant messages. Tool dumps stay out: they
// drown hits in JSON keys.
func (s *Store) IndexThread(id string) error {
	th, err := s.GetThread(id)
	if err != nil {
		return err
	}
	msgs, err := s.ListMessages(id)
	if err != nil {
		return err
	}
	doc := searchDoc{
		ThreadID: id,
		Title:    th.Title,
		Body:     searchBody(th, msgs),
	}
	var existing searchDoc
	err = s.db.Where("thread_id = ?", id).First(&existing).Error
	switch {
	case err == nil:
		if err := s.db.Model(&existing).Updates(map[string]any{
			"title": doc.Title,
			"body":  doc.Body,
		}).Error; err != nil {
			return fmt.Errorf("store: update search doc: %w", err)
		}
		return nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		if err := s.db.Create(&doc).Error; err != nil {
			return fmt.Errorf("store: insert search doc: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("store: lookup search doc: %w", err)
	}
}

func searchBody(th *Thread, msgs []Message) string {
	var b strings.Builder
	if g := strings.TrimSpace(th.Goal); g != "" {
		b.WriteString(g)
		b.WriteByte('\n')
	}
	for _, m := range msgs {
		if !indexableRole(m.Role) {
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		b.WriteString(text)
		b.WriteByte('\n')
		if utf8.RuneCountInString(b.String()) >= maxSearchBodyRunes {
			break
		}
	}
	return b.String()
}

func indexableRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "assistant":
		return true
	default:
		return false
	}
}

func deleteSearchForThread(tx *gorm.DB, id string) error {
	if err := tx.Where("thread_id = ?", id).Delete(&searchDoc{}).Error; err != nil {
		return err
	}
	return tx.Where("thread_id = ?", id).Delete(&SearchChunk{}).Error
}

// ListThreadIDs is every live conversation, for a search backfill.
func (s *Store) ListThreadIDs() ([]string, error) {
	var ids []string
	err := s.db.Model(&Thread{}).Where("archived = ?", false).Pluck("id", &ids).Error
	if err != nil {
		return nil, fmt.Errorf("store: list thread ids: %w", err)
	}
	return ids, nil
}

// DeleteSearchChunksNotIn drops stale passages after a rewind or compact.
func (s *Store) DeleteSearchChunksNotIn(threadID, model string, keep []string) error {
	q := s.db.Where("thread_id = ? AND model = ?", threadID, model)
	if len(keep) > 0 {
		q = q.Where("chunk_key NOT IN ?", keep)
	}
	return q.Delete(&SearchChunk{}).Error
}

// DeleteSearchChunksNotModel drops vectors from a previous embedding pin.
func (s *Store) DeleteSearchChunksNotModel(model string) error {
	return s.db.Where("model <> ?", model).Delete(&SearchChunk{}).Error
}

// SearchThreads ranks conversations by keyword. FTS5 handles substring-y
// latin and long CJK; queries shorter than three runes fall back to LIKE so
// a two-character CJK needle still hits. Archived rows stay out.
func (s *Store) SearchThreads(query string, limit int) ([]ThreadSearchHit, error) {
	q := searchNeedle(query)
	if q == "" {
		return nil, nil
	}
	limit = clampSearchLimit(limit)

	seen := map[string]bool{}
	var out []ThreadSearchHit

	if utf8.RuneCountInString(q) >= ftsMinRunes {
		ftsHits, err := s.searchFTS(q, limit)
		if err != nil {
			return nil, err
		}
		for _, h := range ftsHits {
			if seen[h.ThreadID] {
				continue
			}
			seen[h.ThreadID] = true
			out = append(out, h)
		}
	}
	if len(out) < limit {
		likeHits, err := s.searchLike(q, limit)
		if err != nil {
			return nil, err
		}
		for _, h := range likeHits {
			if seen[h.ThreadID] {
				continue
			}
			seen[h.ThreadID] = true
			out = append(out, h)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

type ftsRow struct {
	ThreadID string
	Title    string
	Snippet  string
}

func (s *Store) searchFTS(q string, limit int) ([]ThreadSearchHit, error) {
	var rows []ftsRow
	err := s.db.Raw(`
		SELECT d.thread_id AS thread_id, t.title AS title,
			snippet(thread_fts, 1, '', '', '…', 16) AS snippet
		FROM thread_fts
		JOIN search_docs d ON d.id = thread_fts.rowid
		JOIN threads t ON t.id = d.thread_id
		WHERE thread_fts MATCH ? AND t.archived = ?
		ORDER BY bm25(thread_fts)
		LIMIT ?`, ftsPhrase(q), false, limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: fts search: %w", err)
	}
	out := make([]ThreadSearchHit, 0, len(rows))
	for _, r := range rows {
		snip := strings.TrimSpace(r.Snippet)
		if snip == "" {
			snip = r.Title
		}
		out = append(out, ThreadSearchHit{ThreadID: r.ThreadID, Title: r.Title, Snippet: snip})
	}
	return out, nil
}

type likeRow struct {
	ThreadID string
	Title    string
	Body     string
	DocTitle string
}

func (s *Store) searchLike(q string, limit int) ([]ThreadSearchHit, error) {
	pat := "%" + likeEscape(q) + "%"
	var rows []likeRow
	err := s.db.Raw(`
		SELECT d.thread_id AS thread_id, t.title AS title, d.body AS body, d.title AS doc_title
		FROM search_docs d
		JOIN threads t ON t.id = d.thread_id
		WHERE t.archived = ? AND (d.title LIKE ? ESCAPE '\' OR d.body LIKE ? ESCAPE '\')
		ORDER BY t.last_active_at DESC
		LIMIT ?`, false, pat, pat, limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: like search: %w", err)
	}
	out := make([]ThreadSearchHit, 0, len(rows))
	for _, r := range rows {
		out = append(out, ThreadSearchHit{
			ThreadID: r.ThreadID,
			Title:    r.Title,
			Snippet:  likeSnippet(r.Title, r.Body, q),
		})
	}
	return out, nil
}

func searchNeedle(q string) string {
	var b strings.Builder
	for _, r := range q {
		switch r {
		case '"', '*', '(', ')', '{', '}', ':', '^':
			b.WriteByte(' ')
		default:
			if r < 32 {
				continue
			}
			b.WriteRune(r)
		}
	}
	parts := strings.Fields(b.String())
	keep := make([]string, 0, len(parts))
	for _, p := range parts {
		switch strings.ToLower(p) {
		case "and", "or", "not", "near":
			continue
		default:
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, " ")
}

func clampSearchLimit(n int) int {
	if n <= 0 {
		return defaultSearchLimit
	}
	if n > maxSearchLimit {
		return maxSearchLimit
	}
	return n
}

func ftsPhrase(q string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range q {
		switch r {
		case '"', '*', '(', ')', '{', '}', ':', '^':
			continue
		default:
			if r < 32 {
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func likeEscape(q string) string {
	q = strings.ReplaceAll(q, `\`, `\\`)
	q = strings.ReplaceAll(q, `%`, `\%`)
	q = strings.ReplaceAll(q, `_`, `\_`)
	return q
}

func likeSnippet(title, body, q string) string {
	if snip, ok := runeWindow(body, q, snippetPadRunes); ok {
		return snip
	}
	if strings.Contains(strings.ToLower(title), strings.ToLower(q)) {
		return title
	}
	if body != "" {
		runes := []rune(body)
		if len(runes) > snippetPadRunes*2 {
			return string(runes[:snippetPadRunes*2]) + "…"
		}
		return body
	}
	return title
}

func runeWindow(text, needle string, pad int) (string, bool) {
	if text == "" || needle == "" {
		return "", false
	}
	lower := strings.ToLower(text)
	idx := strings.Index(lower, strings.ToLower(needle))
	if idx < 0 {
		return "", false
	}
	prefix := utf8.RuneCountInString(text[:idx])
	nlen := utf8.RuneCountInString(text[idx : idx+len(needle)])
	runes := []rune(text)
	start := prefix - pad
	if start < 0 {
		start = 0
	}
	end := prefix + nlen + pad
	if end > len(runes) {
		end = len(runes)
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out, true
}
