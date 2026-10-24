package store

import (
	"database/sql"
	"fmt"
	"slices"
	"strconv"
)

// Link kinds. A link row reads "from <kind> to": from blocks to, from relates to.
const (
	LinkBlocks  = "blocks"
	LinkRelates = "relates"
)

// linkLabels are the forward and inverse labels per kind.
var linkLabels = map[string][2]string{
	LinkBlocks:  {"blocks", "is blocked by"},
	LinkRelates: {"relates to", "relates to"},
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
	// BlockedByAll and BlocksAll are the keys of tasks that transitively block this task, and that it transitively blocks.
	BlockedByAll []string `json:"blocked_by_all"`
	BlocksAll    []string `json:"blocks_all"`
	// Ancestors and Descendants are the keys of all tasks above and below this one in the parent tree.
	Ancestors   []string `json:"ancestors"`
	Descendants []string `json:"descendants"`
}

// reachKeys runs a recursive query that yields task ids and returns their keys in id order.
func reachKeys(db *sql.DB, cte string, args ...any) ([]string, error) {
	rows, err := db.Query(`WITH RECURSIVE r(id) AS (`+cte+`) SELECT t.key FROM r JOIN tasks t ON t.id=r.id ORDER BY t.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("reach: %w", err)
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("scan reach: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// blockReach returns the keys of tasks reachable from id over blocks links: the ones that
// transitively block it (upstream) or that it transitively blocks (downstream).
func blockReach(db *sql.DB, id int64, upstream bool) ([]string, error) {
	near, far := "to_id", "from_id"
	if upstream {
		near, far = far, near
	}
	return reachKeys(db, `SELECT `+near+` FROM task_links WHERE kind=? AND `+far+`=?
 UNION SELECT l.`+near+` FROM task_links l JOIN r ON l.`+far+`=r.id WHERE l.kind=?`, LinkBlocks, id, LinkBlocks)
}

// treeReach returns the keys of id's ancestors (up) or descendants (down) over parent_id.
func treeReach(db *sql.DB, id int64, up bool) ([]string, error) {
	if up {
		return reachKeys(db, `SELECT parent_id FROM tasks WHERE id=? AND parent_id IS NOT NULL
 UNION SELECT t.parent_id FROM tasks t JOIN r ON t.id=r.id WHERE t.parent_id IS NOT NULL`, id)
	}
	return reachKeys(db, `SELECT id FROM tasks WHERE parent_id=? UNION SELECT t.id FROM tasks t JOIN r ON t.parent_id=r.id`, id)
}

// SetParent makes parentID (nil: none) the parent of childID. Keys don't change. A task can't
// become a sub-task of itself or of one of its own descendants.
func SetParent(db *sql.DB, childID int64, parentID *int64) error {
	if parentID != nil {
		var pkey string
		if err := db.QueryRow(`SELECT key FROM tasks WHERE id=?`, *parentID).Scan(&pkey); err != nil {
			return fmt.Errorf("parent task %d: %w", *parentID, err)
		}
		down, err := treeReach(db, childID, false)
		if err != nil {
			return err
		}
		if *parentID == childID || slices.Contains(down, pkey) {
			return fmt.Errorf("parent loop: %s is the task itself or one of its sub-tasks", pkey)
		}
	}
	res, err := db.Exec(`UPDATE tasks SET parent_id=?, updated_at=? WHERE id=?`, parentID, now(), childID)
	if err != nil {
		return fmt.Errorf("set parent: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no such task: %w", sql.ErrNoRows)
	}
	return nil
}

// AddLink records "from <kind> to". A task can't link to itself, and a blocks link may not close a loop.
func AddLink(db *sql.DB, fromID, toID int64, kind string) error {
	if !ValidLinkKind(kind) {
		return fmt.Errorf("unknown link kind %q (blocks|relates)", kind)
	}
	if fromID == toID {
		return fmt.Errorf("a task can't link to itself")
	}
	if kind == LinkBlocks {
		var fromKey string
		if err := db.QueryRow(`SELECT key FROM tasks WHERE id=?`, fromID).Scan(&fromKey); err != nil {
			return fmt.Errorf("add link: %w", err)
		}
		down, err := blockReach(db, toID, false)
		if err != nil {
			return err
		}
		if slices.Contains(down, fromKey) {
			return fmt.Errorf("blocking loop: %s is already blocked by the task it would block", fromKey)
		}
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
	if rel.BlockedByAll, err = blockReach(db, t.ID, true); err != nil {
		return rel, err
	}
	if rel.BlocksAll, err = blockReach(db, t.ID, false); err != nil {
		return rel, err
	}
	if rel.Ancestors, err = treeReach(db, t.ID, true); err != nil {
		return rel, err
	}
	if rel.Descendants, err = treeReach(db, t.ID, false); err != nil {
		return rel, err
	}
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
