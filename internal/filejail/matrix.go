package filejail

import "strings"

// MatrixCase is one row of docs/files-path-matrix.html. The matrix document,
// the table-driven jail tests and the drills all key off MatrixCases:
// TestMatrixCasesComplete asserts the document contains exactly these IDs,
// so the three can never silently drift apart.
type MatrixCase struct {
	ID      string // stable "X-01" / "L-01" id shared with the html matrix
	Illegal bool   // true = must be rejected
	Symlink bool   // true = requires a symlink fixture (handled by dedicated tests)
	Input   string // raw input relative to /tmp/dockerview-files
	Expect  string // human-readable expectation
}

// DefaultJailRoot is the root used by the matrix and tests.
const DefaultJailRoot = "/tmp/dockerview-files"

// MatrixCases returns every legal/illegal path case. Keep order and IDs in
// sync with docs/files-path-matrix.html (enforced by test).
func MatrixCases() []MatrixCase {
	return []MatrixCase{
		// ---- illegal (19) -------------------------------------------------
		{ID: "X-01", Illegal: true, Input: "../escape", Expect: "cleaned path escapes root"},
		{ID: "X-02", Illegal: true, Input: "sub/../../etc/passwd", Expect: "mid '..' escapes root"},
		{ID: "X-03", Illegal: true, Input: "/etc/passwd", Expect: "absolute path outside root"},
		{ID: "X-04", Illegal: true, Input: "/tmp/dockerview-files2/../dockerview-files-evil/x", Expect: "sibling-prefix trick"},
		{ID: "X-05", Illegal: true, Input: "/tmp/dockerview-files/..", Expect: "resolves to parent of root"},
		{ID: "X-06", Illegal: true, Input: "a/../../b", Expect: "cleaned ../b outside root"},
		{ID: "X-07", Illegal: true, Input: "bad\x00name", Expect: "NUL byte"},
		{ID: "X-08", Illegal: true, Input: `C:\Windows\system32`, Expect: "Windows drive + backslash"},
		{ID: "X-09", Illegal: true, Input: "C:/Windows", Expect: "Windows drive variant"},
		{ID: "X-10", Illegal: true, Input: `\\server\share`, Expect: "UNC / backslash"},
		{ID: "X-11", Illegal: true, Input: `..\..\Windows`, Expect: "backslash traversal"},
		{ID: "X-12", Illegal: true, Input: strings.Repeat("a", MaxSegmentLen+50), Expect: "segment overlong"},
		{ID: "X-13", Illegal: true, Symlink: true, Input: "link/file", Expect: "mid-component symlink -> /etc"},
		{ID: "X-14", Illegal: true, Symlink: true, Input: "evil", Expect: "trailing symlink -> /etc/passwd"},
		{ID: "X-15", Illegal: true, Symlink: true, Input: "loop/a", Expect: "symlink loop"},
		{ID: "X-16", Illegal: true, Symlink: true, Input: "outdir", Expect: "archive dir symlink -> /"},
		{ID: "X-17", Illegal: true, Input: "", Expect: "empty path"},
		{ID: "X-18", Illegal: true, Input: DefaultJailRoot, Expect: "root itself is not a file target"},
		{ID: "X-19", Illegal: true, Input: "sub/../../../etc", Expect: "multi-level traversal"},
		// ---- legal (10) ---------------------------------------------------
		{ID: "L-01", Input: "a.txt", Expect: DefaultJailRoot + "/a.txt"},
		{ID: "L-02", Input: "sub/b.txt", Expect: DefaultJailRoot + "/sub/b.txt"},
		{ID: "L-03", Input: DefaultJailRoot + "/c.txt", Expect: DefaultJailRoot + "/c.txt"},
		{ID: "L-04", Input: "./d.txt", Expect: DefaultJailRoot + "/d.txt"},
		{ID: "L-05", Input: "sub/../e.txt", Expect: DefaultJailRoot + "/e.txt"},
		{ID: "L-06", Input: "sub/./f.txt", Expect: DefaultJailRoot + "/sub/f.txt"},
		{ID: "L-07", Input: "deep/nested/dir/g.txt", Expect: DefaultJailRoot + "/deep/nested/dir/g.txt"},
		{ID: "L-08", Input: "日志-文件.txt", Expect: DefaultJailRoot + "/日志-文件.txt"},
		{ID: "L-09", Input: ".", Expect: DefaultJailRoot + " (list root)"},
		{ID: "L-10", Input: "sub", Expect: DefaultJailRoot + "/sub (archive subdir)"},
	}
}
