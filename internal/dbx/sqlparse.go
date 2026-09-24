package dbx

import (
	"fmt"
	"strconv"
	"strings"
)

// sqlToken is one token of a statement. Keywords and unquoted identifiers
// have kind 'w' and their text as written; quoted identifiers kind 'q' with
// the quotes removed. pos and end are byte offsets into the statement.
type sqlToken struct {
	kind     byte // 'w' word, 'q' quoted identifier, 's' string, 'n' number, 'p' punctuation
	text     string
	pos, end int
}

func (t sqlToken) is(word string) bool { return t.kind == 'w' && strings.EqualFold(t.text, word) }

func (t sqlToken) ident() bool { return t.kind == 'w' || t.kind == 'q' }

func (t sqlToken) punct(p string) bool { return t.kind == 'p' && t.text == p }

// tokenize splits stmt into tokens, dropping comments (# only starts one in
// MySQL; in PostgreSQL it is an operator). ok is false for input it can't make
// sense of, such as an unterminated string.
func tokenize(stmt, driver string) (toks []sqlToken, ok bool) {
	s := stmt
	i := 0
	add := func(kind byte, text string, start, end int) {
		toks = append(toks, sqlToken{kind, text, start, end})
	}
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '-' && strings.HasPrefix(s[i:], "--"), c == '#' && driver == MySQL:
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return nil, false
			}
			i += end + 4
		case c == '\'':
			j := i + 1
			var b strings.Builder
			for {
				if j >= len(s) {
					return nil, false
				}
				if s[j] == '\\' && j+1 < len(s) {
					b.WriteByte(s[j+1])
					j += 2
					continue
				}
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						b.WriteByte('\'')
						j += 2
						continue
					}
					break
				}
				b.WriteByte(s[j])
				j++
			}
			add('s', b.String(), i, j+1)
			i = j + 1
		case c == '"' || c == '`' || c == '[':
			closer := c
			if c == '[' {
				closer = ']'
			}
			j := i + 1
			var b strings.Builder
			for {
				if j >= len(s) {
					return nil, false
				}
				if s[j] == closer {
					if closer != ']' && j+1 < len(s) && s[j+1] == closer {
						b.WriteByte(closer)
						j += 2
						continue
					}
					break
				}
				b.WriteByte(s[j])
				j++
			}
			add('q', b.String(), i, j+1)
			i = j + 1
		case c == '$' && i+1 < len(s) && (s[i+1] == '$' || isWordByte(s[i+1]) && !isDigit(s[i+1])):
			// PostgreSQL dollar quoting: $$...$$ or $tag$...$tag$.
			end := strings.IndexByte(s[i+1:], '$')
			if end < 0 {
				return nil, false
			}
			tag := s[i : i+end+2]
			if strings.Trim(tag[1:len(tag)-1], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
				add('p', "$", i, i+1)
				i++
				continue
			}
			rest := s[i+len(tag):]
			close := strings.Index(rest, tag)
			if close < 0 {
				return nil, false
			}
			add('s', rest[:close], i, i+len(tag)+close+len(tag))
			i += len(tag) + close + len(tag)
		case isDigit(c):
			j := i
			for j < len(s) && (isWordByte(s[j]) || s[j] == '.') {
				j++
			}
			add('n', s[i:j], i, j)
			i = j
		case isWordByte(c) || c >= 0x80:
			j := i
			for j < len(s) && (isWordByte(s[j]) || s[j] == '$' || s[j] >= 0x80) {
				j++
			}
			add('w', s[i:j], i, j)
			i = j
		default:
			add('p', string(c), i, i+1)
			i++
		}
	}
	return toks, true
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isWordByte(c byte) bool {
	return c == '_' || isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// selectItem is one entry of a select list.
type selectItem struct {
	star   bool   // * or t.*
	column string // plain column reference, "" for an expression
	quoted bool   // column was a quoted identifier
	name   string // result column name when predictable (alias or column)
}

// simpleSelect is a SELECT reading one table.
type simpleSelect struct {
	items  []selectItem
	schema string // "" when unqualified
	table  string
	// ref is the table reference as written, for PostgreSQL's to_regclass.
	ref string

	// Byte ranges of the statement, for rewriting it: head is everything up
	// to and including the table and its alias; the clauses follow.
	head                string
	where, order, limit string // clause bodies, without their keyword
	offset, fetch, lock string
	// limitN is the row count of a plain "LIMIT n", else 0.
	limitN int
}

// Reasons a result set is read-only, shown to the user.
const (
	roNotSimple = "only results of a SELECT from a single table can be edited"
	roNoKey     = "the table has no primary key or unique key without NULLs"
)

// parseSimpleSelect recognizes SELECT <columns> FROM <table> [alias]
// [WHERE …] [ORDER BY …] [LIMIT …] [OFFSET …] [FOR …]: one table, no joins,
// grouping, DISTINCT or set operations. It returns a reason when stmt is
// anything else.
func parseSimpleSelect(stmt, driver string) (*simpleSelect, string) {
	toks, ok := tokenize(stmt, driver)
	if !ok {
		return nil, roNotSimple
	}
	for len(toks) > 0 && toks[len(toks)-1].punct(";") {
		toks = toks[:len(toks)-1]
	}
	if len(toks) < 4 || !toks[0].is("select") {
		return nil, roNotSimple
	}
	i := 1
	if toks[i].is("distinct") {
		return nil, "results of SELECT DISTINCT can't be edited"
	}
	if toks[i].is("all") {
		i++
	}

	// Split the select list on top-level commas, up to FROM.
	var items [][]sqlToken
	var cur []sqlToken
	depth := 0
	for ; i < len(toks); i++ {
		t := toks[i]
		if depth == 0 && t.is("from") {
			break
		}
		switch {
		case t.punct("("):
			depth++
		case t.punct(")"):
			depth--
		case t.punct(",") && depth == 0:
			items = append(items, cur)
			cur = nil
			continue
		}
		cur = append(cur, t)
	}
	if i >= len(toks) || len(cur) == 0 {
		return nil, roNotSimple
	}
	items = append(items, cur)
	i++ // FROM

	sel := &simpleSelect{}
	for _, it := range items {
		sel.items = append(sel.items, parseSelectItem(it))
	}

	// Table reference: name or schema.name.
	if i >= len(toks) || !toks[i].ident() {
		return nil, roNotSimple
	}
	refStart := i
	sel.table = toks[i].text
	i++
	if i+1 < len(toks) && toks[i].punct(".") && toks[i+1].ident() {
		sel.schema, sel.table = sel.table, toks[i+1].text
		i += 2
	}
	sel.ref = rebuild(toks[refStart:i])
	if i < len(toks) && toks[i].punct("(") {
		return nil, roNotSimple // table function
	}
	// Optional alias.
	if i < len(toks) && toks[i].is("as") {
		i++
	}
	if i < len(toks) && toks[i].ident() && clauseKeyword(toks, i) == "" {
		i++
	}
	sel.head = strings.TrimRight(stmt[:toks[i-1].end], " \t\r\n")

	// The rest may only be WHERE, ORDER BY, LIMIT, OFFSET, FETCH and FOR,
	// in that order, each at most once.
	type clause struct {
		name       string
		start, end int // token range of the body
	}
	var clauses []clause
	depth = 0
	for j := i; j < len(toks); j++ {
		t := toks[j]
		switch {
		case t.punct("("):
			depth++
			continue
		case t.punct(")"):
			depth--
			continue
		case t.punct(";"):
			return nil, roNotSimple // more than one statement
		}
		if depth > 0 {
			continue
		}
		for _, w := range []string{"join", "group", "having", "union", "intersect", "except", "window", "from"} {
			if t.is(w) {
				return nil, roNotSimple
			}
		}
		if name := clauseKeyword(toks, j); name != "" {
			if name == "order" {
				j++ // ORDER BY
			}
			clauses = append(clauses, clause{name: name, start: j + 1})
		} else if j == i {
			return nil, roNotSimple // something other than a clause after the table
		}
	}
	order := map[string]int{"where": 0, "order": 1, "limit": 2, "offset": 3, "fetch": 4, "for": 5}
	last := -1
	for k := range clauses {
		if o := order[clauses[k].name]; o <= last {
			return nil, roNotSimple
		} else {
			last = o
		}
		clauses[k].end = len(toks)
		if k+1 < len(clauses) {
			clauses[k].end = clauses[k+1].start - 1
			if clauses[k+1].name == "order" {
				clauses[k].end--
			}
		}
		body := ""
		if clauses[k].start < clauses[k].end {
			body = strings.TrimSpace(stmt[toks[clauses[k].start].pos:toks[clauses[k].end-1].end])
		}
		switch clauses[k].name {
		case "where":
			sel.where = body
		case "order":
			sel.order = body
		case "limit":
			sel.limit = body
			if n, err := strconv.Atoi(body); err == nil {
				sel.limitN = n
			}
		case "offset":
			sel.offset = body
		case "fetch":
			sel.fetch = body
		case "for":
			sel.lock = body
		}
	}
	return sel, ""
}

// clauseKeyword returns the clause toks[i] starts (where, order, limit,
// offset, fetch, for), or "".
func clauseKeyword(toks []sqlToken, i int) string {
	t := toks[i]
	switch {
	case t.is("where"), t.is("limit"), t.is("offset"), t.is("fetch"), t.is("for"):
		return strings.ToLower(t.text)
	case t.is("order") && i+1 < len(toks) && toks[i+1].is("by"):
		return "order"
	}
	return ""
}

func parseSelectItem(it []sqlToken) selectItem {
	isDot := func(t sqlToken) bool { return t.punct(".") }
	isStar := func(t sqlToken) bool { return t.punct("*") }
	switch {
	case len(it) == 1 && isStar(it[0]):
		return selectItem{star: true}
	case len(it) == 3 && it[0].ident() && isDot(it[1]) && isStar(it[2]):
		return selectItem{star: true}
	}
	// col, t.col, s.t.col — optionally followed by [AS] alias.
	n := 1
	for n+1 < len(it) && isDot(it[n]) && it[n+1].ident() {
		n += 2
	}
	if !it[0].ident() || it[0].kind == 'w' && isKeywordish(it[0].text) {
		return selectItem{name: aliasOf(it)}
	}
	col := it[n-1]
	rest := it[n:]
	item := selectItem{column: col.text, quoted: col.kind == 'q', name: col.text}
	switch {
	case len(rest) == 0:
	case len(rest) == 1 && rest[0].ident():
		item.name = rest[0].text
	case len(rest) == 2 && rest[0].is("as") && rest[1].ident():
		item.name = rest[1].text
	default:
		return selectItem{name: aliasOf(it)}
	}
	return item
}

// aliasOf returns the alias of an expression item ("expr AS x" or "expr x"),
// or "" when its result name can't be predicted.
func aliasOf(it []sqlToken) string {
	n := len(it)
	if n >= 3 && it[n-2].is("as") && it[n-1].ident() {
		return it[n-1].text
	}
	return ""
}

func isKeywordish(w string) bool {
	switch strings.ToLower(w) {
	case "case", "cast", "not", "null", "true", "false", "exists", "distinct", "interval", "date", "time", "timestamp":
		return true
	}
	return false
}

// rebuild writes tokens back as SQL, for a table reference.
func rebuild(toks []sqlToken) string {
	var b strings.Builder
	for _, t := range toks {
		switch t.kind {
		case 'q':
			b.WriteString(doubleQuote(t.text))
		default:
			b.WriteString(t.text)
		}
	}
	return b.String()
}

// BrowseOptions changes a simple SELECT for browsing a table: an extra filter,
// another sort order and a page of rows.
type BrowseOptions struct {
	// Filter is a condition added to the WHERE clause, e.g. "name LIKE 'A%'".
	Filter string `json:"filter"`
	// OrderBy is the 1-based result column to sort on; 0 keeps the query's
	// own ORDER BY.
	OrderBy int  `json:"orderBy"`
	Desc    bool `json:"desc"`
	// Limit and Offset select a page; Limit 0 keeps the query's own limit.
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// Browsable describes a result that BrowseSQL can filter, sort and page.
type Browsable struct {
	// Limit is the query's own LIMIT n (0 when it has none), the page size
	// for loading more rows.
	Limit int `json:"limit"`
}

// browsable reports whether stmt can be rewritten by BrowseSQL.
func browsable(stmt, driver string) *Browsable {
	sel, _ := parseSimpleSelect(stmt, driver)
	if sel == nil {
		return nil
	}
	return &Browsable{Limit: sel.limitN}
}

// BrowseSQL rewrites a simple SELECT (see parseSimpleSelect) with opt. The
// table's columns, when known, make the filter forgiving about case and
// quotes on PostgreSQL (see normalizeFilter).
func BrowseSQL(stmt, driver string, opt BrowseOptions, columns []FilterColumn) (string, error) {
	sel, reason := parseSimpleSelect(stmt, driver)
	if sel == nil {
		return "", fmt.Errorf("can't filter or sort this query: %s", reason)
	}
	var b strings.Builder
	b.WriteString(sel.head)
	filter := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(opt.Filter), ";"))
	filter = normalizeFilter(filter, driver, columns)
	switch {
	case sel.where != "" && filter != "":
		fmt.Fprintf(&b, "\nWHERE (%s) AND (%s)", sel.where, filter)
	case sel.where != "":
		b.WriteString("\nWHERE " + sel.where)
	case filter != "":
		b.WriteString("\nWHERE " + filter)
	}
	switch {
	case opt.OrderBy > 0:
		fmt.Fprintf(&b, "\nORDER BY %d", opt.OrderBy)
		if opt.Desc {
			b.WriteString(" DESC")
		}
	case sel.order != "":
		b.WriteString("\nORDER BY " + sel.order)
	}
	if opt.Limit > 0 {
		fmt.Fprintf(&b, "\nLIMIT %d", opt.Limit)
		if opt.Offset > 0 {
			fmt.Fprintf(&b, " OFFSET %d", opt.Offset)
		}
	} else {
		if sel.limit != "" {
			b.WriteString("\nLIMIT " + sel.limit)
		}
		if sel.offset != "" {
			b.WriteString("\nOFFSET " + sel.offset)
		}
		if sel.fetch != "" {
			b.WriteString("\nFETCH " + sel.fetch)
		}
	}
	if sel.lock != "" {
		b.WriteString("\nFOR " + sel.lock)
	}
	return b.String(), nil
}
