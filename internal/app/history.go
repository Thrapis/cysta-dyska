package app

import (
	"errors"
	"time"

	"cysta-dyska/internal/scan"
	"cysta-dyska/internal/store"
	"cysta-dyska/internal/treemap"
)

// defaultHotspotMinDelta is the smallest change listed as a hotspot.
const defaultHotspotMinDelta = 10 << 20

// maxTiles caps the treemap tiles for one folder; the rest become one tile.
const maxTiles = 150

// SnapshotDTO is one saved scan.
type SnapshotDTO struct {
	ID         int64 `json:"id"`
	TakenAt    int64 `json:"takenAt"` // unix milliseconds
	Alloc      int64 `json:"alloc"`
	Files      int64 `json:"files"`
	DurationMs int64 `json:"durationMs"`
}

// HistoryDTO lists the saved scans of the current folder.
type HistoryDTO struct {
	Snapshots  []SnapshotDTO `json:"snapshots"`  // newest first, same method as the current scan
	Method     string        `json:"method"`     // "walk" (normal scan) or "mft" (full scan)
	CurrentID  int64         `json:"currentId"`  // the scan on screen; 0 if not saved
	MinAlloc   int64         `json:"minAlloc"`   // smaller entries are not stored
	HotspotMin int64         `json:"hotspotMin"` // smallest change listed as a hotspot
	Warning    string        `json:"warning,omitempty"`
}

// DiffRowDTO is one entry compared between two scans.
type DiffRowDTO struct {
	Key         string `json:"key"`
	Path        string `json:"path"` // relative to the scanned folder
	IsDir       bool   `json:"isDir"`
	Before      int64  `json:"before"`
	After       int64  `json:"after"`
	Delta       int64  `json:"delta"`
	BeforeKnown bool   `json:"beforeKnown"`
	AfterKnown  bool   `json:"afterKnown"`
	HasChildren bool   `json:"hasChildren"`
}

// TileDTO is one rectangle of the treemap.
type TileDTO struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	IsDir       bool    `json:"isDir"`
	Alloc       int64   `json:"alloc"`
	HasChildren bool    `json:"hasChildren"`
	IsRest      bool    `json:"isRest"`
	Count       int     `json:"count"` // entries folded into a rest tile
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	W           float64 `json:"w"`
	H           float64 `json:"h"`
}

// saveHistory stores a finished scan. Failure only loses history, never the
// scan, so it is reported as a warning.
func (a *App) saveHistory(tree *scan.Node, took time.Duration, method string) (id int64, warning string) {
	if a.history == nil {
		return 0, "history is not available"
	}
	id, err := a.history.Save(tree, took, method, a.historyOpts)
	if err != nil {
		return 0, "could not save this scan to history: " + err.Error()
	}
	return id, ""
}

// History lists the saved scans of the folder currently on screen.
func (a *App) History() (HistoryDTO, error) {
	a.mu.Lock()
	tree, id, warning, method := a.tree, a.snapshotID, a.historyWarning, a.method
	a.mu.Unlock()
	if tree == nil {
		return HistoryDTO{}, errors.New("no scan results yet")
	}
	h := HistoryDTO{
		Snapshots: []SnapshotDTO{}, Method: method, CurrentID: id, MinAlloc: a.historyOpts.MinAlloc,
		HotspotMin: a.hotspotMinDelta, Warning: warning,
	}
	if a.history == nil {
		return h, nil
	}
	list, err := a.history.Snapshots(tree.Path(), method)
	if err != nil {
		h.Warning = err.Error()
		return h, nil
	}
	for _, s := range list {
		h.Snapshots = append(h.Snapshots, SnapshotDTO{
			ID: s.ID, TakenAt: s.TakenAt.UnixMilli(), Alloc: s.Alloc, Files: s.Files,
			DurationMs: s.Duration.Milliseconds(),
		})
	}
	return h, nil
}

// CompareChildren compares the entries inside folder parentKey ("" for the
// scanned folder) between two saved scans, largest change first.
func (a *App) CompareChildren(before, after int64, parentKey string) ([]DiffRowDTO, error) {
	if a.history == nil {
		return nil, errors.New("history is not available")
	}
	rows, err := a.history.Compare(before, after, parentKey)
	if err != nil {
		return nil, err
	}
	return diffDTOs(rows), nil
}

// Hotspots lists the places that changed most between two saved scans.
func (a *App) Hotspots(before, after int64) ([]DiffRowDTO, error) {
	if a.history == nil {
		return nil, errors.New("history is not available")
	}
	rows, err := a.history.Hotspots(before, after, a.hotspotMinDelta, 30)
	if err != nil {
		return nil, err
	}
	return diffDTOs(rows), nil
}

func diffDTOs(rows []store.DiffRow) []DiffRowDTO {
	out := make([]DiffRowDTO, len(rows))
	for i, r := range rows {
		out[i] = DiffRowDTO{
			Key: r.Key, Path: r.Path, IsDir: r.IsDir,
			Before: r.Before, After: r.After, Delta: r.Delta(),
			BeforeKnown: r.BeforeKnown, AfterKnown: r.AfterKnown, HasChildren: r.HasChildren,
		}
	}
	return out
}

// Treemap lays out the entries of the folder at path from the last scan in
// a width x height area.
func (a *App) Treemap(path string, width, height float64) ([]TileDTO, error) {
	a.mu.Lock()
	tree := a.tree
	a.mu.Unlock()
	if tree == nil {
		return nil, errors.New("no scan results yet")
	}
	n := tree.Find(path)
	if n == nil || !n.IsDir {
		return nil, errors.New("folder is not in the last scan")
	}

	var tiles []TileDTO
	var values []int64
	for i, c := range n.Children { // largest first
		if c.Alloc <= 0 {
			break
		}
		if i == maxTiles {
			rest := TileDTO{Path: n.Path(), IsRest: true}
			for _, r := range n.Children[i:] {
				rest.Alloc += r.Alloc
				rest.Count++
			}
			tiles = append(tiles, rest)
			values = append(values, rest.Alloc)
			break
		}
		tiles = append(tiles, TileDTO{
			Name: c.Name, Path: c.Path(), IsDir: c.IsDir, Alloc: c.Alloc,
			HasChildren: c.IsDir && !c.Link && len(c.Children) > 0,
		})
		values = append(values, c.Alloc)
	}
	for i, r := range treemap.Squarify(values, treemap.Rect{W: width, H: height}) {
		tiles[i].X, tiles[i].Y, tiles[i].W, tiles[i].H = r.X, r.Y, r.W, r.H
	}
	if tiles == nil {
		tiles = []TileDTO{}
	}
	return tiles, nil
}
