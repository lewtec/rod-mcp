package types

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/go-rod/rod"
	"gopkg.in/yaml.v3"

	"github.com/aliwatters/rod-mcp/types/js"
	"github.com/aliwatters/rod-mcp/utils"
)

var (
	// refPattern matches [ref=...] suffixes in ARIA snapshot scalars.
	refPattern = regexp.MustCompile(`\[ref=(.*?)\]`)
	// iframeRefPattern matches iframe ref prefixes like "f1e42".
	iframeRefPattern = regexp.MustCompile(`^f(\d+)(.*)`)
)

const snapshotTpl = `
- Page URL: {{ .URL }}
- Page Title: {{ .Title }}
- Frame Count: {{ .Frames }}
- Page Snapshot
` + "```yaml\n" + "{{ .Snapshot }}" + "\n```\n"

// RefEntry stores metadata about an interactive element in the ARIA snapshot.
type RefEntry struct {
	Ref  string // e.g. "e42" or "f1e42"
	Role string // e.g. "button", "link", "textbox"
	Name string // accessible name, e.g. "Login"
	Raw  string // full scalar value
}

type Snapshot struct {
	frames       []*rod.Page
	textSnapshot string
	refIndex     []RefEntry
	// refAccum is used during walk() to accumulate ref entries in a single pass.
	// It is set to nil after walk() completes and the entries are moved to refIndex.
	refAccum *[]RefEntry
}

func BuildSnapshot(p *rod.Page, compact bool) (*Snapshot, error) {
	snapshot := &Snapshot{
		frames: []*rod.Page{},
	}
	yamlDoc, err := snapshot.captureSnapshotWithFrames(p)
	if err != nil {
		return nil, err
	}

	// refIndex is populated during walk() via walkScalarNode accumulator.
	// No separate buildRefIndex pass is needed.

	if compact {
		yamlDoc = compactSnapshot(yamlDoc)
	}

	yamlBytes, err := yaml.Marshal(yamlDoc)
	if err != nil {
		return nil, fmt.Errorf("snapshot yaml marshal: %w", err)
	}

	pageInfo, err := p.Info()
	if err != nil {
		return nil, fmt.Errorf("snapshot page info: %w", err)
	}

	tplInfo := map[string]any{
		"URL":      pageInfo.URL,
		"Title":    pageInfo.Title,
		"Snapshot": strings.TrimSpace(string(yamlBytes)),
		"Frames":   len(snapshot.frames),
	}
	res, err := utils.ExecuteTemplate(snapshotTpl, tplInfo)
	if err != nil {
		return nil, fmt.Errorf("snapshot template exec: %w", err)
	}
	snapshot.textSnapshot = res
	return snapshot, nil
}

func (s *Snapshot) String() string {
	return s.textSnapshot
}

func (s *Snapshot) captureSnapshotWithFrames(p *rod.Page) (*yaml.Node, error) {
	// Initialize the ref accumulator on the first (top-level) call.
	isRoot := s.refAccum == nil
	if isRoot {
		entries := make([]RefEntry, 0)
		s.refAccum = &entries
	}

	s.frames = append(s.frames, p)
	frameIndex := len(s.frames) - 1

	rawSnapshot, err := p.Eval(js.AriaSnapshot, "document.body", "({ref: true})")
	if err != nil {
		return nil, fmt.Errorf("snapshot frame %d eval: %w", frameIndex, err)
	}

	var snapNode yaml.Node

	err = yaml.Unmarshal([]byte(rawSnapshot.Value.String()), &snapNode)
	if err != nil {
		return nil, fmt.Errorf("snapshot frame %d yaml unmarshal: %w", frameIndex, err)
	}

	result, walkErr := s.walk(&snapNode, frameIndex, p)

	// On the root call, move accumulated refs to refIndex and clear the accumulator.
	if isRoot {
		s.refIndex = *s.refAccum
		s.refAccum = nil
	}

	return result, walkErr
}

func (s *Snapshot) walk(node *yaml.Node, frameIndex int, frame *rod.Page) (*yaml.Node, error) {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		processedRoot, err := s.walk(node.Content[0], frameIndex, frame)
		if err != nil {
			return nil, err
		}
		node.Content[0] = processedRoot
		return node, nil
	}

	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			newKey, err := s.walk(node.Content[i], frameIndex, frame)
			if err != nil {
				return nil, err
			}
			newValue, err := s.walk(node.Content[i+1], frameIndex, frame)
			if err != nil {
				return nil, err
			}
			node.Content[i] = newKey
			node.Content[i+1] = newValue
		}

	case yaml.SequenceNode:
		for i, item := range node.Content {
			processed, err := s.walk(item, frameIndex, frame)
			if err != nil {
				return nil, err
			}
			node.Content[i] = processed
		}

	case yaml.ScalarNode:
		return s.walkScalarNode(node, frameIndex, frame)
	}

	return node, nil
}

// walkScalarNode processes scalar nodes: adjusts frame refs, expands iframe snapshots,
// and accumulates ref index entries in a single pass.
func (s *Snapshot) walkScalarNode(node *yaml.Node, frameIndex int, frame *rod.Page) (*yaml.Node, error) {
	if node.Tag != "!!str" {
		return node, nil
	}
	s.adjustFrameRef(node, frameIndex)
	if result := s.tryExpandIframe(node, frame); result != nil {
		return result, nil
	}
	s.accumulateRef(node.Value)
	return node, nil
}

// adjustFrameRef prefixes element refs with the frame index for cross-frame disambiguation.
func (s *Snapshot) adjustFrameRef(node *yaml.Node, frameIndex int) {
	if frameIndex > 0 {
		node.Value = strings.Replace(node.Value, "[ref=", fmt.Sprintf("[ref=f%d", frameIndex), 1)
	}
}

// tryExpandIframe captures an iframe's snapshot inline if the node represents an iframe.
// Returns the expanded node, or nil if the node is not an iframe.
func (s *Snapshot) tryExpandIframe(node *yaml.Node, frame *rod.Page) *yaml.Node {
	if strings.HasPrefix(node.Value, "iframe ") {
		return s.walkIframeNode(node, frame)
	}
	return nil
}

// accumulateRef adds a ref index entry if the node value contains a ref pattern.
func (s *Snapshot) accumulateRef(value string) {
	if s.refAccum != nil {
		if entry, ok := parseRefScalar(value); ok {
			*s.refAccum = append(*s.refAccum, entry)
		}
	}
}

// walkIframeNode captures an iframe's snapshot and returns a mapping node with the result.
// Returns nil if the node doesn't have a ref or isn't an iframe.
func (s *Snapshot) walkIframeNode(node *yaml.Node, frame *rod.Page) *yaml.Node {
	matches := refPattern.FindStringSubmatch(node.Value)
	if len(matches) <= 1 {
		return nil
	}

	ref := matches[1]
	// Strip optional frame prefix (e.g. "f1e42" → "e42") that adjustFrameRef
	// may have added before tryExpandIframe is called. The DOM element still
	// has the original unprefixed ref.
	if fm := iframeRefPattern.FindStringSubmatch(ref); len(fm) > 0 {
		ref = fm[2]
	}
	fallback := &yaml.Node{Kind: yaml.ScalarNode, Value: "<could not capture iframe snapshot>"}
	pairNode := &yaml.Node{
		Kind:    yaml.MappingNode,
		Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: node.Value}, fallback},
	}

	childFrameEle, err := utils.QueryEleByAria(frame, ref)
	if err != nil {
		slog.Debug(fmt.Sprintf("walkIframeNode: query element for ref %q: %s", ref, err))
		return pairNode
	}
	childFrame, err := childFrameEle.Frame()
	if err != nil {
		slog.Debug(fmt.Sprintf("walkIframeNode: get frame for ref %q: %s", ref, err))
		return pairNode
	}
	childSnapshot, err := s.captureSnapshotWithFrames(childFrame)
	if err != nil || len(childSnapshot.Content) == 0 {
		if err != nil {
			slog.Debug(fmt.Sprintf("walkIframeNode: capture snapshot for ref %q: %s", ref, err))
		}
		return pairNode
	}

	pairNode.Content[1] = childSnapshot.Content[0]
	return pairNode
}
