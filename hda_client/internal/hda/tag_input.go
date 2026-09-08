package hda

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const maxExpressionTags = 100_000

type tagExpression struct {
	prefix string
	suffix string
	width  int
	from   int
	to     int
}

// ExpandTagExpression accepts only prefix{index:Nd}suffix[from,to]. It is a
// formatter, not an evaluator: no arbitrary code is ever executed.
func ExpandTagExpression(source string) ([]string, error) {
	expr, err := parseTagExpression(source)
	if err != nil {
		return nil, err
	}
	tags := make([]string, expr.to-expr.from+1)
	for i := range tags {
		tags[i] = expr.prefix + fmt.Sprintf("%0*d", expr.width, expr.from+i) + expr.suffix
	}
	return tags, nil
}

// PreviewTagExpression validates without shipping a large tag list to the UI.
// Second 是第二个位号, 供前端展示 "前2后1" 的样例。
func PreviewTagExpression(source string) (TagExpressionPreview, error) {
	expr, err := parseTagExpression(source)
	if err != nil {
		return TagExpressionPreview{}, err
	}
	format := func(i int) string { return expr.prefix + fmt.Sprintf("%0*d", expr.width, i) + expr.suffix }
	second := expr.from
	if expr.to > expr.from {
		second = expr.from + 1
	}
	return TagExpressionPreview{
		Count:  expr.to - expr.from + 1,
		First:  format(expr.from),
		Second: format(second),
		Last:   format(expr.to),
	}, nil
}

func parseTagExpression(source string) (tagExpression, error) {
	s := strings.TrimSpace(source)
	open := strings.Index(s, "{index:")
	if open < 0 {
		return tagExpression{}, fmt.Errorf("format must be M{index:4d}.VALUE[0,9999]")
	}
	close := strings.Index(s[open:], "}")
	if close < 0 {
		return tagExpression{}, fmt.Errorf("missing closing brace")
	}
	close += open
	format := s[open+len("{index:") : close]
	if len(format) < 2 || !strings.HasSuffix(format, "d") {
		return tagExpression{}, fmt.Errorf("index format must be width followed by d")
	}
	width, err := strconv.Atoi(strings.TrimSuffix(format, "d"))
	if err != nil || width < 1 || width > 6 {
		return tagExpression{}, fmt.Errorf("index width must be between 1 and 6")
	}
	rangeStart := strings.Index(s[close+1:], "[")
	if rangeStart < 0 || !strings.HasSuffix(s, "]") {
		return tagExpression{}, fmt.Errorf("missing range [from,to]")
	}
	rangeStart += close + 1
	if strings.Count(s, "{index:") != 1 || strings.Count(s, "[") != 1 {
		return tagExpression{}, fmt.Errorf("expression may contain one index and one range")
	}
	parts := strings.Split(strings.TrimSuffix(s[rangeStart+1:], "]"), ",")
	if len(parts) != 2 {
		return tagExpression{}, fmt.Errorf("range must be [from,to]")
	}
	from, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return tagExpression{}, fmt.Errorf("invalid range start")
	}
	to, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return tagExpression{}, fmt.Errorf("invalid range end")
	}
	if from < 0 || to < 0 || from > to {
		return tagExpression{}, fmt.Errorf("range must be non-negative and ordered")
	}
	max := 1
	for range width {
		max *= 10
	}
	if to >= max {
		return tagExpression{}, fmt.Errorf("index:%dd supports 0 through %d", width, max-1)
	}
	if to-from+1 > maxExpressionTags {
		return tagExpression{}, fmt.Errorf("expression expands to more than %d tags", maxExpressionTags)
	}
	return tagExpression{prefix: s[:open], suffix: s[close+1 : rangeStart], width: width, from: from, to: to}, nil
}

// ParseCSVTags accepts the supplied template's `node` header (or `tag` for
// compatibility), strips a UTF-8 BOM, and uses encoding/csv for quoted fields.
func ParseCSVTags(r io.Reader) ([]string, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	var tags []string
	seen := map[string]struct{}{}
	firstRecord := true
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid CSV: %w", err)
		}
		if len(record) == 0 {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(record[0], "\ufeff"))
		if firstRecord {
			firstRecord = false
			if strings.EqualFold(value, "node") || strings.EqualFold(value, "tag") {
				continue
			}
		}
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		tags = append(tags, value)
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("CSV has no tags")
	}
	return tags, nil
}
