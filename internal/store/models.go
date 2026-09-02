package store

// File is a stored file's metadata. The bytes live in a content-addressed blob
// named by Hash; File.Name is display-only and never used as a path.
type File struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // file | screenshot
	MIME      string `json:"mime"`
	Size      int64  `json:"size"`
	Hash      string `json:"hash"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// ClipboardItem is one entry in the shared clipboard.
type ClipboardItem struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Format    string `json:"format"` // text | code | url | markdown
	Pinned    bool   `json:"pinned"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// Snippet is a titled, language-tagged block of code or text.
type Snippet struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Language  string `json:"language"`
	Pinned    bool   `json:"pinned"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// Note is a lightweight markdown note.
type Note struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Pinned    bool   `json:"pinned"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// UploadSession tracks one resumable (tus) upload for the Transfers tab.
type UploadSession struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	MIME      string `json:"mime"`
	Size      int64  `json:"size"`
	Offset    int64  `json:"offset"`
	Status    string `json:"status"` // active | completed | failed
	FileID    string `json:"file_id,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// Share is a public, tokenised link to a file. The secret token is never
// stored; only its SHA-256 (TokenHash) is kept.
type Share struct {
	ID           string `json:"id"`
	TokenHash    string `json:"-"`
	FileID       string `json:"file_id"`
	CreatedAt    int64  `json:"created_at"`
	ExpiresAt    *int64 `json:"expires_at,omitempty"`
	MaxDownloads *int64 `json:"max_downloads,omitempty"`
	Downloads    int64  `json:"downloads"`
	OneTime      bool   `json:"one_time"`
	RevokedAt    *int64 `json:"revoked_at,omitempty"`
}

// SearchHit is one global-search result row.
type SearchHit struct {
	Entity  string `json:"entity"` // file | clipboard | snippet | note
	RefID   string `json:"ref_id"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// Page is a generic pagination envelope.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	Total      int64  `json:"total"`
}
