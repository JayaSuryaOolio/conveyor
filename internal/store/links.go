package store

import (
	"database/sql"
	"fmt"
	"strconv"
)

// Link kinds. A link row reads "from <kind> to": from blocks to, from relates to, from duplicates to.
const (
	LinkBlocks     = "blocks"
	LinkRelates    = "relates"
	LinkDuplicates = "duplicates"
)

// linkLabels are the forward and inverse labels per kind.
var linkLabels = map[string][2]string{
	LinkBlocks:     {"blocks", "is blocked by"},
	LinkRelates:    {"relates to", "relates to"},
	LinkDuplicates: {"duplicates", "is duplicated by"},
}

// ValidLinkKind reports whether kind is one of the link kinds.
func ValidLinkKind(kind string) bool { _, ok := linkLabels[kind]; return ok }

// ChildKey is the key of the n-th (1-based) child of parent. Levels alternate
// between letters and digits so keys stay unambiguous: CV-1 -> CV-1A..CV-1Z,
// CV-1AA.. (bijective base-26); CV-1A -> CV-1A1, CV-1A2..; CV-1A1 -> CV-1A1A..
func ChildKey(parent string, n int) string {
	if c := parent[len(parent)-1]; c < '0' || c > '9' {
		return parent + strconv.Itoa(n)
	}
	var s string
	for ; n > 0; n /= 26 {
		n--
		s = string(rune('A'+n%26)) + s
	}
	return parent + s
}

// nextChildKey is the first unused child key of the task with id parentID and key parentKey.
func nextChildKey(tx *sql.Tx, parentID int64, parentKey string) (string, error) {
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM tasks WHERE parent_id=?`, parentID).Scan(&n); err != nil {
		return "", fmt.Errorf("count children: %w", err)
	}
	for {
		n++
		key := ChildKey(parentKey, n)
		var one int
		switch err := tx.QueryRow(`SELECT 1 FROM tasks WHERE key=?`, key).Scan(&one); err {
		case sql.ErrNoRows:
			return key, nil
		case nil:
		default:
			return "", fmt.Errorf("check key: %w", err)
		}
	}
}

// TaskRef is a task as shown in a relation.
type TaskRef struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Status Status `json:"status"`
}

// Relation is a link seen from one task. Label already accounts for direction.
type Relation struct {
	Kind  string  `json:"kind"`
	Label string  `json:"label"`
	From  bool    `json:"from"` // true: this task is the link's from side
	Task  TaskRef `json:"task"`
}

// Relations is a task's parent, children and links.
type Relations struct {
	Parent   *TaskRef   `json:"parent"`
	Children []TaskRef  `json:"children"`
	Links    []Relation `json:"links"`
}

// AddLink records "from <kind> to". A task can't link to itself.
func AddLink(db *sql.DB, fromID, toID int64, kind string) error {
	if !ValidLinkKind(kind) {
		return fmt.Errorf("unknown link kind %q (blocks|relates|duplicates)", kind)
	}
	if fromID == toID {
		return fmt.Errorf("a task can't link to itself")
	}
	if _, err := db.Exec(`INSERT INTO task_links (from_id, to_id, kind) VALUES (?,?,?)`, fromID, toID, kind); err != nil {
		return fmt.Errorf("add link: %w", err)
	}
	return nil
}

// RemoveLink deletes "from <kind> to"; for the symmetric relates kind either direction matches.
func RemoveLink(db *sql.DB, fromID, toID int64, kind string) error {
	res, err := db.Exec(`DELETE FROM task_links WHERE kind=? AND ((from_id=? AND to_id=?) OR (?=? AND from_id=? AND to_id=?))`,
		kind, fromID, toID, kind, LinkRelates, toID, fromID)
	if err != nil {
		return fmt.Errorf("remove link: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no such link: %w", sql.ErrNoRows)
	}
	return nil
}

// OpenBlockers returns the keys of tasks that block taskID and are not done or cancelled.
func OpenBlockers(db *sql.DB, taskID int64) ([]string, error) {
	rows, err := db.Query(`SELECT t.key FROM task_links l JOIN tasks t ON t.id=l.from_id
 WHERE l.kind=? AND l.to_id=? AND t.status NOT IN (?,?) ORDER BY t.id`, LinkBlocks, taskID, StatusDone, StatusCancelled)
	if err != nil {
		return nil, fmt.Errorf("open blockers: %w", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("scan blocker: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func listRefs(db *sql.DB, q string, args ...any) ([]TaskRef, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskRef{}
	for rows.Next() {
		var r TaskRef
		if err := rows.Scan(&r.Key, &r.Title, &r.Status); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListRelations returns t's parent, children and links.
func ListRelations(db *sql.DB, t Task) (Relations, error) {
	rel := Relations{Links: []Relation{}}
	var err error
	if t.ParentID != nil {
		ps, err := listRefs(db, `SELECT key, title, status FROM tasks WHERE id=?`, *t.ParentID)
		if err != nil {
			return rel, fmt.Errorf("parent: %w", err)
		}
		if len(ps) > 0 {
			rel.Parent = &ps[0]
		}
	}
	if rel.Children, err = listRefs(db, `SELECT key, title, status FROM tasks WHERE parent_id=? ORDER BY id`, t.ID); err != nil {
		return rel, fmt.Errorf("children: %w", err)
	}
	rows, err := db.Query(`SELECT l.kind, 1, t.key, t.title, t.status FROM task_links l JOIN tasks t ON t.id=l.to_id WHERE l.from_id=?
 UNION ALL SELECT l.kind, 0, t.key, t.title, t.status FROM task_links l JOIN tasks t ON t.id=l.from_id WHERE l.to_id=?`, t.ID, t.ID)
	if err != nil {
		return rel, fmt.Errorf("links: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r Relation
		if err := rows.Scan(&r.Kind, &r.From, &r.Task.Key, &r.Task.Title, &r.Task.Status); err != nil {
			return rel, fmt.Errorf("scan link: %w", err)
		}
		if r.From {
			r.Label = linkLabels[r.Kind][0]
		} else {
			r.Label = linkLabels[r.Kind][1]
		}
		rel.Links = append(rel.Links, r)
	}
	return rel, rows.Err()
}
