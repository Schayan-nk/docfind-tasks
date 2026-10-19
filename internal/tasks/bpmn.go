package tasks

// BPMN 2.0 XML in and out of a process, so a map can be drawn in any BPMN
// tool (bpmn.io, Camunda Modeler and the like) and brought here, or taken from
// here to one of them.
//
// Out: a start event before the first step, a userTask per step, an
// exclusive or parallel gateway per gateway, an end event per end (and one
// for the routes that just stop), a sequence flow per route with its
// condition as text, and the diagram from the designer's positions. What
// BPMN has no words for -- who a step goes to, its questions, a route's
// rules and order -- rides along in each element's extensionElements as
// <docfind:data>{json}</docfind:data>, so docfind -> BPMN -> docfind loses
// nothing; other tools keep or ignore it.
//
// In: every kind of task is a step, a sub-process or call activity a
// subprocess, an exclusive/event-based/complex gateway a first-true gateway,
// an inclusive or parallel one an all-true gateway, an intermediate event a
// plain pass-through; a lane becomes the group a step goes to, for an
// administrator to map. Conditions are read in docfind's condition language
// (cond.go); one that is not is dropped and said so. A route into a parallel
// gateway that several reach waits there for the others (join).

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// bpmnEl is any XML element, kept whole: BPMN files mix element kinds and
// prefixes freely, so they are walked by local name.
type bpmnEl struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Kids    []bpmnEl   `xml:",any"`
	Text    string     `xml:",chardata"`
}

func (e bpmnEl) attr(name string) string {
	for _, a := range e.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func (e bpmnEl) kid(local string) (bpmnEl, bool) {
	for _, k := range e.Kids {
		if k.XMLName.Local == local {
			return k, true
		}
	}
	return bpmnEl{}, false
}

// walk visits e and everything under it.
func (e bpmnEl) walk(f func(bpmnEl)) {
	f(e)
	for _, k := range e.Kids {
		k.walk(f)
	}
}

// bpmnData is docfind's own part of an element.
type bpmnData struct {
	Assign *Assign     `json:"assign,omitempty"`
	Fields []FormField `json:"fields,omitempty"`
	Kind   string      `json:"kind,omitempty"`
	Order  int         `json:"order,omitempty"`
	When   string      `json:"when,omitempty"`
	Rules  []FieldRule `json:"rules,omitempty"`
	ID     string      `json:"id,omitempty"`
}

func (e bpmnEl) docfind() (bpmnData, bool) {
	var d bpmnData
	ext, ok := e.kid("extensionElements")
	if !ok {
		return d, false
	}
	x, ok := ext.kid("data")
	if !ok || x.XMLName.Space != docfindNS {
		return d, false
	}
	return d, json.Unmarshal([]byte(x.Text), &d) == nil
}

const docfindNS = "https://github.com/Schayan-nk/docfind-tasks/bpmn"

var bpmnTaskKinds = map[string]bool{"task": true, "userTask": true, "manualTask": true, "serviceTask": true, "scriptTask": true,
	"sendTask": true, "receiveTask": true, "businessRuleTask": true}
var bpmnSubKinds = map[string]bool{"subProcess": true, "callActivity": true, "transaction": true, "adHocSubProcess": true}

// ImportBPMN reads a BPMN 2.0 file as a process. Warnings say what was read
// differently from how the file meant it; the map is checked again when it
// is saved.
func ImportBPMN(data []byte) (Process, []string, error) {
	var root bpmnEl
	if err := xml.Unmarshal(data, &root); err != nil {
		return Process{}, nil, Invalid{"فایل BPMN خوانده نشد: " + err.Error()}
	}
	var proc bpmnEl
	found := false
	root.walk(func(e bpmnEl) {
		if found || e.XMLName.Local != "process" {
			return
		}
		for _, k := range e.Kids {
			if k.XMLName.Local == "startEvent" || bpmnTaskKinds[k.XMLName.Local] {
				proc, found = e, true
				return
			}
		}
	})
	if !found {
		return Process{}, nil, Invalid{"در فایل فرآیندی با رویداد آغاز یا گام پیدا نشد."}
	}
	var warn []string
	p := Process{Name: oneLine(proc.attr("name"), 120)}
	if p.Name == "" {
		root.walk(func(e bpmnEl) {
			if p.Name == "" && e.XMLName.Local == "participant" && e.attr("processRef") == proc.attr("id") {
				p.Name = oneLine(e.attr("name"), 120)
			}
		})
	}

	// Where each element was drawn, and which lane it is in.
	at := map[string][2]float64{}
	root.walk(func(e bpmnEl) {
		if e.XMLName.Local != "BPMNShape" {
			return
		}
		if b, ok := e.kid("Bounds"); ok {
			x, _ := strconv.ParseFloat(b.attr("x"), 64)
			y, _ := strconv.ParseFloat(b.attr("y"), 64)
			w, _ := strconv.ParseFloat(b.attr("width"), 64)
			h, _ := strconv.ParseFloat(b.attr("height"), 64)
			at[e.attr("bpmnElement")] = [2]float64{x + w/2, y + h/2}
		}
	})
	lane := map[string]string{}
	proc.walk(func(e bpmnEl) {
		if e.XMLName.Local != "lane" {
			return
		}
		for _, k := range e.Kids {
			if k.XMLName.Local == "flowNodeRef" {
				lane[strings.TrimSpace(k.Text)] = oneLine(e.attr("name"), 80)
			}
		}
	})

	ids := map[string]string{}  // BPMN id -> node id
	gate := map[string]string{} // BPMN id -> its gateway kind
	starts := map[string]bool{}
	var flows []bpmnEl
	defaults := map[string]string{} // element -> its default flow
	for _, k := range proc.Kids {
		kind, local := "", k.XMLName.Local
		switch {
		case local == "startEvent":
			starts[k.attr("id")] = true
			continue
		case local == "sequenceFlow":
			flows = append(flows, k)
			continue
		case bpmnTaskKinds[local]:
			kind = "step"
		case bpmnSubKinds[local]:
			kind = "subprocess"
		case local == "exclusiveGateway" || local == "eventBasedGateway" || local == "complexGateway":
			kind, gate[k.attr("id")] = "gateway", "xor"
		case local == "inclusiveGateway":
			kind, gate[k.attr("id")] = "gateway", "par"
		case local == "parallelGateway":
			kind, gate[k.attr("id")] = "gateway", "parallel"
		case local == "endEvent" && k.attr("id") == "end_docfind":
			ids["end_docfind"] = "" // docfind's own: routes that just stop
			continue
		case local == "endEvent":
			kind = "end"
		case local == "intermediateCatchEvent" || local == "intermediateThrowEvent":
			kind, gate[k.attr("id")] = "gateway", "xor"
			warn = append(warn, "رویداد میانی «"+nameOr(k)+"» به‌صورت گذر ساده خوانده شد؛ زمان‌سنج و پیام آن اجرا نمی‌شود.")
		case local == "boundaryEvent":
			warn = append(warn, "رویداد مرزی «"+nameOr(k)+"» خوانده نشد.")
			continue
		default:
			continue
		}
		bid := k.attr("id")
		n := Node{ID: nodeID(bid), Kind: kind, Name: oneLine(k.attr("name"), 160)}
		if (kind == "step" || kind == "subprocess") && n.Name == "" {
			n.Name = bid
		}
		if c, ok := at[bid]; ok {
			n.X, n.Y = c[0], c[1]
		}
		if g := gate[bid]; g != "" {
			n.Gate = map[string]string{"parallel": "par"}[g]
			if n.Gate == "" {
				n.Gate = g
			}
		}
		if kind == "step" || kind == "subprocess" {
			n.Assign = Assign{Mode: "requester"}
			if l := lane[bid]; l != "" {
				n.Assign = Assign{Mode: "users", Group: l}
			}
		}
		if d, ok := k.docfind(); ok {
			if d.Assign != nil {
				n.Assign = *d.Assign
			}
			n.Fields = d.Fields
			if d.ID != "" {
				n.ID = d.ID
			}
		}
		if def := k.attr("default"); def != "" {
			defaults[bid] = def
		}
		ids[bid] = n.ID
		p.Nodes = append(p.Nodes, n)
	}

	// Routes: from the start event, the first step; the rest as they are.
	type route struct {
		e    Edge
		at   int  // its place in the file, kept in the map
		mine bool // docfind's own kind and order came with it
	}
	out := map[string][]route{}
	into := map[string]int{}
	var placed []Edge
	var order []string
	for _, f := range flows {
		src, dst := f.attr("sourceRef"), f.attr("targetRef")
		if starts[src] {
			if p.Start == "" {
				p.Start = ids[dst]
			}
			continue
		}
		from, ok := ids[src]
		if !ok {
			continue
		}
		to, ok := ids[dst]
		if !ok {
			warn = append(warn, "مسیری از «"+from+"» به چیزی که خوانده نشد کنار گذاشته شد.")
			continue
		}
		e := Edge{From: from, To: to}
		mine := false
		if d, ok := f.docfind(); ok {
			e.Kind, e.Order, e.When, e.Rules, mine = d.Kind, d.Order, d.When, d.Rules, true
		} else if c, ok := f.kid("conditionExpression"); ok {
			e.When = bpmnCondition(c.Text)
			if _, err := ParseExpr(e.When); err != nil {
				warn = append(warn, "شرط مسیر «"+nameOf(p, from)+"» ← «"+nameOf(p, to)+"» خوانده نشد و برداشته شد: "+strings.TrimSpace(c.Text))
				e.When = ""
			}
		}
		if defaults[src] == f.attr("id") && !mine {
			e.When, e.Order = "", 1<<20 // the default goes last, taken when nothing else is
		}
		if len(out[src]) == 0 {
			order = append(order, src)
		}
		out[src] = append(out[src], route{e, len(placed), mine})
		placed = append(placed, Edge{})
		into[dst]++
	}
	if p.Start == "" {
		for _, n := range p.Nodes {
			if n.Kind == "step" && into[bpmnIDOf(ids, n.ID)] == 0 {
				p.Start = n.ID
				break
			}
		}
		warn = append(warn, "رویداد آغازی پیدا نشد؛ نخستین گامی که مسیری به آن نمی‌رسد آغاز گرفته شد.")
	}
	for _, src := range order {
		rs := out[src]
		sort.SliceStable(rs, func(i, j int) bool {
			oi, oj := rs[i].e.Order, rs[j].e.Order
			if oi == 0 {
				oi = i + 1
			}
			if oj == 0 {
				oj = j + 1
			}
			return oi < oj
		})
		for i, r := range rs {
			e := r.e
			if !r.mine {
				e.Order = i + 1
				dst := bpmnIDOf(ids, e.To)
				switch {
				case gate[dst] == "parallel" && into[dst] >= 2 && len(rs) == 1:
					e.Kind = "join"
				case (gate[src] == "par" || gate[src] == "parallel") && len(rs) > 1:
					e.Kind = "par"
				case len(rs) > 1 || e.When != "":
					e.Kind = "xor"
				default:
					e.Kind = "seq"
				}
			}
			placed[r.at] = e
		}
	}
	p.Edges = placed
	return p, warn, nil
}

// bpmnCondition is a condition as BPMN tools write it, in docfind's words:
// ${x == 1} and #{...} unwrapped.
func bpmnCondition(s string) string {
	s = strings.TrimSpace(s)
	if (strings.HasPrefix(s, "${") || strings.HasPrefix(s, "#{")) && strings.HasSuffix(s, "}") {
		s = strings.TrimSpace(s[2 : len(s)-1])
	}
	return s
}

func nameOr(e bpmnEl) string {
	if n := strings.TrimSpace(e.attr("name")); n != "" {
		return n
	}
	return e.attr("id")
}

func nameOf(p Process, id string) string {
	if n, ok := p.node(id); ok {
		return nodeName(n)
	}
	return id
}

// bpmnIDOf is the BPMN id a node id came from.
func bpmnIDOf(ids map[string]string, node string) string {
	for b, n := range ids {
		if n == node {
			return b
		}
	}
	return node
}

// nodeID is a BPMN id as a node id: up to 64 characters.
func nodeID(bid string) string {
	if len(bid) <= 64 {
		return bid
	}
	sum := sha256.Sum256([]byte(bid))
	return bid[:47] + "_" + hex.EncodeToString(sum[:8])
}

// ------------------------------------------------------------- export

var ncName = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// bpmnSize is how big each kind of node is drawn.
func bpmnSize(kind string) (float64, float64) {
	switch kind {
	case "gateway":
		return 50, 50
	case "end":
		return 36, 36
	}
	return 150, 60
}

// ExportBPMN writes a process as BPMN 2.0 XML with its diagram.
func ExportBPMN(p Process) ([]byte, error) {
	if len(p.Nodes) == 0 {
		return nil, Invalid{"فرآیند گامی ندارد."}
	}
	xid := map[string]string{}
	used := map[string]bool{}
	for _, n := range p.Nodes {
		id := "n_" + ncName.ReplaceAllString(n.ID, "_")
		for base, i := id, 2; used[id]; i++ {
			id = base + "_" + strconv.Itoa(i)
		}
		used[id] = true
		xid[n.ID] = id
	}
	gateKind := func(n Node) string {
		if n.Gate != "" {
			return n.Gate
		}
		for _, e := range p.Edges {
			if (e.From == n.ID && e.Kind == "par") || (e.To == n.ID && e.Kind == "join") {
				return "par"
			}
		}
		return "xor"
	}
	type flow struct {
		id       string
		e        Edge
		from, to string
	}
	var flows []flow
	needEnd := false
	for i, e := range p.Edges {
		to := xid[e.To]
		if e.To == "" {
			to, needEnd = "end_docfind", true
		}
		flows = append(flows, flow{"f" + strconv.Itoa(i+1), e, xid[e.From], to})
	}
	var b bytes.Buffer
	esc := func(s string) string {
		var w bytes.Buffer
		_ = xml.EscapeText(&w, []byte(s))
		return w.String()
	}
	data := func(d bpmnData) string {
		j, _ := json.Marshal(d)
		return `<bpmn:extensionElements><docfind:data>` + esc(string(j)) + `</docfind:data></bpmn:extensionElements>`
	}
	ios := func(id string) string {
		var s strings.Builder
		for _, f := range flows {
			if f.to == id {
				s.WriteString("<bpmn:incoming>" + f.id + "</bpmn:incoming>")
			}
		}
		if id == xid[p.Start] {
			s.WriteString("<bpmn:incoming>f_start</bpmn:incoming>")
		}
		for _, f := range flows {
			if f.from == id {
				s.WriteString("<bpmn:outgoing>" + f.id + "</bpmn:outgoing>")
			}
		}
		return s.String()
	}
	b.WriteString(xml.Header)
	b.WriteString(`<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL" xmlns:bpmndi="http://www.omg.org/spec/BPMN/20100524/DI" ` +
		`xmlns:dc="http://www.omg.org/spec/DD/20100524/DC" xmlns:di="http://www.omg.org/spec/DD/20100524/DI" ` +
		`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:docfind="` + docfindNS + `" ` +
		`id="docfind_definitions" targetNamespace="` + docfindNS + `" exporter="docfind" exporterVersion="` + strconv.Itoa(p.Version) + `">` + "\n")
	b.WriteString(`<bpmn:process id="docfind_process" name="` + esc(p.Name) + `" isExecutable="true">` + "\n")
	b.WriteString(`<bpmn:startEvent id="start_docfind" name="آغاز"><bpmn:outgoing>f_start</bpmn:outgoing></bpmn:startEvent>` + "\n")
	for _, n := range p.Nodes {
		id := xid[n.ID]
		switch n.Kind {
		case "gateway":
			tag := "exclusiveGateway"
			if gateKind(n) == "par" {
				tag = "parallelGateway"
			}
			b.WriteString(`<bpmn:` + tag + ` id="` + id + `" name="` + esc(n.Name) + `">` + data(bpmnData{ID: n.ID}) + ios(id) + `</bpmn:` + tag + ">\n")
		case "end":
			b.WriteString(`<bpmn:endEvent id="` + id + `" name="` + esc(n.Name) + `">` + data(bpmnData{ID: n.ID}) + ios(id) + "</bpmn:endEvent>\n")
		default:
			tag := "userTask"
			if n.Kind == "subprocess" {
				tag = "callActivity"
			}
			a := n.Assign
			b.WriteString(`<bpmn:` + tag + ` id="` + id + `" name="` + esc(n.Name) + `">` + data(bpmnData{ID: n.ID, Assign: &a, Fields: n.Fields}) + ios(id) + `</bpmn:` + tag + ">\n")
		}
	}
	if needEnd {
		b.WriteString(`<bpmn:endEvent id="end_docfind" name="پایان">` + ios("end_docfind") + "</bpmn:endEvent>\n")
	}
	b.WriteString(`<bpmn:sequenceFlow id="f_start" sourceRef="start_docfind" targetRef="` + xid[p.Start] + `"/>` + "\n")
	for _, f := range flows {
		cond := f.e.When
		if len(f.e.Rules) > 0 {
			cond = RulesExpr(f.e.Rules)
		}
		b.WriteString(`<bpmn:sequenceFlow id="` + f.id + `" sourceRef="` + f.from + `" targetRef="` + f.to + `">` +
			data(bpmnData{Kind: f.e.Kind, Order: f.e.Order, When: f.e.When, Rules: f.e.Rules}))
		if cond != "" {
			b.WriteString(`<bpmn:conditionExpression xsi:type="bpmn:tFormalExpression">` + esc(cond) + `</bpmn:conditionExpression>`)
		}
		b.WriteString("</bpmn:sequenceFlow>\n")
	}
	b.WriteString("</bpmn:process>\n")

	// The diagram: the designer's places, or a column when there are none.
	pos := map[string][3]float64{} // id -> x, y, kind's height
	placed := false
	for _, n := range p.Nodes {
		if n.X != 0 || n.Y != 0 {
			placed = true
		}
	}
	for i, n := range p.Nodes {
		x, y := n.X, n.Y
		if !placed {
			x, y = 200, 160+float64(i)*110
		}
		_, h := bpmnSize(n.Kind)
		pos[xid[n.ID]] = [3]float64{x, y, h}
	}
	sp := pos[xid[p.Start]]
	pos["start_docfind"] = [3]float64{sp[0], sp[1] - sp[2]/2 - 70, 36}
	if needEnd {
		maxY := 0.0
		for _, v := range pos {
			maxY = math.Max(maxY, v[1])
		}
		pos["end_docfind"] = [3]float64{sp[0], maxY + 110, 36}
	}
	b.WriteString(`<bpmndi:BPMNDiagram id="docfind_diagram"><bpmndi:BPMNPlane id="docfind_plane" bpmnElement="docfind_process">` + "\n")
	shape := func(id string, w, h float64) {
		c := pos[id]
		b.WriteString(fmt.Sprintf(`<bpmndi:BPMNShape id="%s_di" bpmnElement="%s"><dc:Bounds x="%g" y="%g" width="%g" height="%g"/></bpmndi:BPMNShape>`+"\n",
			id, id, c[0]-w/2, c[1]-h/2, w, h))
	}
	shape("start_docfind", 36, 36)
	for _, n := range p.Nodes {
		w, h := bpmnSize(n.Kind)
		shape(xid[n.ID], w, h)
	}
	if needEnd {
		shape("end_docfind", 36, 36)
	}
	edge := func(id, from, to string) {
		a, z := pos[from], pos[to]
		y1, y2 := a[1]+a[2]/2, z[1]-z[2]/2
		if z[1] <= a[1] { // back up the map: out of the side
			y1, y2 = a[1], z[1]
		}
		b.WriteString(fmt.Sprintf(`<bpmndi:BPMNEdge id="%s_di" bpmnElement="%s"><di:waypoint x="%g" y="%g"/><di:waypoint x="%g" y="%g"/></bpmndi:BPMNEdge>`+"\n",
			id, id, a[0], y1, z[0], y2))
	}
	edge("f_start", "start_docfind", xid[p.Start])
	for _, f := range flows {
		edge(f.id, f.from, f.to)
	}
	b.WriteString("</bpmndi:BPMNPlane></bpmndi:BPMNDiagram>\n</bpmn:definitions>\n")
	return b.Bytes(), nil
}

// RulesExpr is a route's rules written in the condition language, for BPMN
// tools to show; docfind itself tests the rules.
func RulesExpr(rules []FieldRule) string {
	q := func(s string) string { return strconv.Quote(s) }
	var parts []string
	for _, r := range rules {
		v := "@@" + r.Field
		one := func(w string) string { return "in_array(" + q(w) + ", " + v + ")" }
		var s string
		switch r.Op {
		case "set":
			s = "!empty(" + v + ")"
		case "unset":
			s = "empty(" + v + ")"
		case "eq":
			s = one(r.Value[0])
		case "ne":
			s = "!" + one(r.Value[0])
		case "in", "nin":
			var any []string
			for _, w := range r.Value {
				any = append(any, one(w))
			}
			s = "(" + strings.Join(any, " || ") + ")"
			if r.Op == "nin" {
				s = "!" + s
			}
		case "gt", "lt":
			n, _ := ruleNumber(r.Value[0])
			s = v + map[string]string{"gt": " > ", "lt": " < "}[r.Op] + strconv.FormatFloat(n, 'f', -1, 64)
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " && ")
}
