package temporalgraph

import (
	"strings"
	"testing"
	"time"
)

func TestRenderDigestMarkdownFrontmatter(t *testing.T) {
	now := time.Now()
	result := DigestResult{
		StartTime: now.Add(-1 * time.Hour),
		EndTime:   now,
		TopEntities: []DigestEntity{
			{Name: "entity1", Count: 5},
		},
		EventTimeline: []DigestEventItem{
			{Time: now.Unix(), Title: "Event 1"},
		},
		FactCount:     10,
		RelationCount: 5,
		SourceCount:   3,
	}

	markdown := RenderDigestMarkdown(result)

	// Check for required frontmatter fields
	requiredFields := []string{
		"---",
		"date:",
		"window_start:",
		"window_end:",
		"source: chatlog temporal graph",
		"tags: [chatlog, graph-digest]",
	}

	for _, field := range requiredFields {
		if !strings.Contains(markdown, field) {
			t.Errorf("Missing frontmatter field: %s", field)
		}
	}
}

func TestRenderDigestMarkdownSectionCount(t *testing.T) {
	now := time.Now()
	result := DigestResult{
		StartTime:     now.Add(-1 * time.Hour),
		EndTime:       now,
		TopEntities:   []DigestEntity{{Name: "entity1", Count: 5}},
		EventTimeline: []DigestEventItem{{Time: now.Unix(), Title: "Event 1"}},
		FactCount:     10,
		RelationCount: 5,
		SourceCount:   3,
	}

	markdown := RenderDigestMarkdown(result)

	// Count H2 sections (##)
	h2Count := strings.Count(markdown, "\n## ")
	if h2Count != 7 {
		t.Errorf("Expected 7 H2 sections, got %d", h2Count)
	}

	// Verify section order
	sections := []string{
		"一句话结论",
		"核心主题",
		"关键实体",
		"事件时间线",
		"新增事实与关系",
		"可复用沉淀",
		"后续跟进",
	}

	for i, section := range sections {
		// Find position of this section
		sectionHeader := "## " + section
		pos := strings.Index(markdown, sectionHeader)
		if pos < 0 {
			t.Errorf("Missing section: %s", section)
			continue
		}

		// Check that next section (if exists) comes after this one
		if i < len(sections)-1 {
			nextSection := "## " + sections[i+1]
			nextPos := strings.Index(markdown, nextSection)
			if nextPos < pos {
				t.Errorf("Section order violation: %s should come before %s", section, sections[i+1])
			}
		}
	}
}

func TestRenderDigestMarkdownEmptyResult(t *testing.T) {
	now := time.Now()
	result := DigestResult{
		StartTime: now.Add(-1 * time.Hour),
		EndTime:   now,
		// All fields are zero/empty
	}

	markdown := RenderDigestMarkdown(result)

	// Verify it renders all 7 sections even when empty
	h2Count := strings.Count(markdown, "\n## ")
	if h2Count != 7 {
		t.Errorf("Expected 7 H2 sections even on empty result, got %d", h2Count)
	}

	// Verify no panic and markdown is non-empty
	if len(markdown) == 0 {
		t.Errorf("Expected non-empty markdown output")
	}

	// Verify frontmatter is still present
	if !strings.HasPrefix(markdown, "---") {
		t.Errorf("Expected markdown to start with YAML frontmatter delimiter")
	}
}

func TestRenderDigestMarkdownEntityRendering(t *testing.T) {
	now := time.Now()
	result := DigestResult{
		StartTime: now.Add(-1 * time.Hour),
		EndTime:   now,
		TopEntities: []DigestEntity{
			{Name: "Alice", Count: 10},
			{Name: "Bob", Count: 5},
		},
		EventTimeline: []DigestEventItem{},
		FactCount:     0,
		RelationCount: 0,
		SourceCount:   0,
	}

	markdown := RenderDigestMarkdown(result)

	// Check for entities in the markdown
	if !strings.Contains(markdown, "Alice") || !strings.Contains(markdown, "10") {
		t.Errorf("Alice entity not rendered correctly")
	}
	if !strings.Contains(markdown, "Bob") || !strings.Contains(markdown, "5") {
		t.Errorf("Bob entity not rendered correctly")
	}
}

func TestRenderDigestMarkdownEventTimeline(t *testing.T) {
	now := time.Now()
	result := DigestResult{
		StartTime: now.Add(-1 * time.Hour),
		EndTime:   now,
		TopEntities: []DigestEntity{},
		EventTimeline: []DigestEventItem{
			{Time: now.Unix(), Title: "Event A"},
			{Time: now.Add(1 * time.Minute).Unix(), Title: "Event B"},
		},
		FactCount:     0,
		RelationCount: 0,
		SourceCount:   0,
	}

	markdown := RenderDigestMarkdown(result)

	// Check for events in the markdown
	if !strings.Contains(markdown, "Event A") || !strings.Contains(markdown, "Event B") {
		t.Errorf("Events not rendered correctly in timeline")
	}
}
