package tasks

import (
	"encoding/xml"
	"slices"
	"strings"
	"testing"
)

// docfind -> BPMN -> docfind gives back the same map: its steps with whom
// they go to and what they ask, its gateways, and every route with its kind,
// order, expression or rules.
func TestABPMNRoundTripKeepsTheMap(t *testing.T) {
	p := miniFlow()
	p.Edges[1].When, p.Edges[1].Rules = "", []FieldRule{{"req_type", "eq", []string{"1"}}, {"amount", "gt", []string{"۵٬۰۰۰"}}}
	p.Nodes[0].X, p.Nodes[0].Y = 400, 120
	p, err := cleanProcess(p)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ExportBPMN(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := xml.Unmarshal(out, new(bpmnEl)); err != nil {
		t.Fatalf("not well-formed: %v", err)
	}
	for _, want := range []string{"<bpmn:userTask", "<bpmn:exclusiveGateway", "<bpmn:parallelGateway", "<bpmn:endEvent id=\"end_docfind\"",
		"<bpmndi:BPMNShape", `in_array(&#34;1&#34;, @@req_type) &amp;&amp; @@amount &gt; 5000`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the file lacks %s", want)
		}
	}
	back, warn, err := ImportBPMN(out)
	if err != nil || len(warn) != 0 {
		t.Fatalf("read back: %v %v", err, warn)
	}
	if back.Name != p.Name || back.Start != p.Start || len(back.Nodes) != len(p.Nodes) || len(back.Edges) != len(p.Edges) {
		t.Fatalf("read back: %+v", back)
	}
	for i, n := range p.Nodes {
		m := back.Nodes[i]
		if m.ID != n.ID || m.Kind != n.Kind || m.Name != n.Name || m.Assign.Mode != n.Assign.Mode || !slices.Equal(m.Assign.Users, n.Assign.Users) ||
			m.Assign.Field != n.Assign.Field || m.Assign.Department != n.Assign.Department || len(m.Fields) != len(n.Fields) {
			t.Errorf("node %d: %+v, was %+v", i, m, n)
		}
	}
	if back.Nodes[0].X != 400 || back.Nodes[0].Y != 120 {
		t.Errorf("the first step was drawn at %v,%v", back.Nodes[0].X, back.Nodes[0].Y)
	}
	gates := map[string]string{}
	for _, n := range back.Nodes {
		gates[n.ID] = n.Gate
	}
	if gates["g1"] != "xor" || gates["g2"] != "par" || gates["g3"] != "par" {
		t.Errorf("gateways: %v", gates)
	}
	for i, e := range p.Edges {
		f := back.Edges[i]
		if f.From != e.From || f.To != e.To || f.Kind != e.Kind || f.When != e.When || f.Order != e.Order || len(f.Rules) != len(e.Rules) {
			t.Errorf("route %d: %+v, was %+v", i, f, e)
		}
	}
	if _, err := cleanProcess(back); err != nil {
		t.Errorf("the map read back is refused: %v", err)
	}
}

// A file drawn elsewhere (bpmn.io's way of writing): tasks, a decision with
// a default, a parallel split and join, a lane, an unreadable condition.
const foreignBPMN = `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL" xmlns:bpmndi="http://www.omg.org/spec/BPMN/20100524/DI"
  xmlns:dc="http://www.omg.org/spec/DD/20100524/DC" id="d1" targetNamespace="x">
 <collaboration id="c1"><participant id="pa" name="خرید کالا" processRef="P1"/></collaboration>
 <process id="P1">
  <laneSet><lane id="L1" name="مالی"><flowNodeRef>pay</flowNodeRef></lane></laneSet>
  <startEvent id="s"/>
  <userTask id="ask" name="ثبت درخواست"/>
  <exclusiveGateway id="big" name="مبلغ؟" default="small"/>
  <task id="ceo" name="تأیید مدیرعامل"/>
  <exclusiveGateway id="m"/>
  <parallelGateway id="split"/>
  <task id="pay" name="پرداخت"/>
  <task id="ship" name="ارسال"/>
  <parallelGateway id="meet"/>
  <boundaryEvent id="late" name="دیرکرد" attachedToRef="pay"/>
  <endEvent id="e" name="پایان"/>
  <sequenceFlow id="f0" sourceRef="s" targetRef="ask"/>
  <sequenceFlow id="f1" sourceRef="ask" targetRef="big"/>
  <sequenceFlow id="small" sourceRef="big" targetRef="m"/>
  <sequenceFlow id="f2" sourceRef="big" targetRef="ceo"><conditionExpression>${amount > 1000}</conditionExpression></sequenceFlow>
  <sequenceFlow id="f3" sourceRef="ceo" targetRef="m"><conditionExpression>${execution.getVariable("ok")}</conditionExpression></sequenceFlow>
  <sequenceFlow id="f9" sourceRef="m" targetRef="split"/>
  <sequenceFlow id="f4" sourceRef="split" targetRef="pay"/>
  <sequenceFlow id="f5" sourceRef="split" targetRef="ship"/>
  <sequenceFlow id="f6" sourceRef="pay" targetRef="meet"/>
  <sequenceFlow id="f7" sourceRef="ship" targetRef="meet"/>
  <sequenceFlow id="f8" sourceRef="meet" targetRef="e"/>
 </process>
 <bpmndi:BPMNDiagram><bpmndi:BPMNPlane bpmnElement="c1">
  <bpmndi:BPMNShape bpmnElement="ask"><dc:Bounds x="100" y="80" width="100" height="80"/></bpmndi:BPMNShape>
 </bpmndi:BPMNPlane></bpmndi:BPMNDiagram>
</definitions>`

func TestABPMNFromElsewhereIsReadAsAProcess(t *testing.T) {
	p, warn, err := ImportBPMN([]byte(foreignBPMN))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "خرید کالا" || p.Start != "ask" || len(p.Nodes) != 9 {
		t.Fatalf("read: %+v", p)
	}
	if n, _ := p.node("ask"); n.X != 150 || n.Y != 120 || n.Kind != "step" {
		t.Errorf("ask: %+v", n)
	}
	if n, _ := p.node("pay"); n.Assign.Group != "مالی" {
		t.Errorf("the lane: %+v", n.Assign)
	}
	if len(warn) != 2 || !strings.Contains(strings.Join(warn, " "), "دیرکرد") || !strings.Contains(strings.Join(warn, " "), "getVariable") {
		t.Errorf("warnings: %q", warn)
	}
	kinds := map[string]string{}
	for _, e := range p.Edges {
		kinds[e.From+">"+e.To] = e.Kind + "|" + e.When
	}
	for route, want := range map[string]string{
		"ask>big": "seq|", "big>ceo": "xor|amount > 1000", "big>m": "xor|", "ceo>m": "seq|", "m>split": "seq|",
		"split>pay": "par|", "split>ship": "par|", "pay>meet": "join|", "ship>meet": "join|", "meet>e": "seq|",
	} {
		if kinds[route] != want {
			t.Errorf("%s: %q, want %q", route, kinds[route], want)
		}
	}
	// The default comes after the route with a condition.
	for _, e := range p.out("big") {
		if e.To != "ceo" {
			t.Errorf("first out of the decision: %+v", e)
		}
		break
	}
	if _, err := cleanProcess(p); err != nil {
		t.Errorf("refused: %v", err)
	}
	if _, _, err := ImportBPMN([]byte("<definitions><process id='x'/></definitions>")); err == nil {
		t.Error("a file with no process was read")
	}
}

// A route made in the designer carries rules, not an expression; the case
// follows them, and saving drops an expression that came with them.
func TestARouteFollowsItsRules(t *testing.T) {
	p := miniFlow()
	for i, e := range p.Edges {
		if e.From == "g1" && e.To == "tender" {
			p.Edges[i].Rules = []FieldRule{{"req_type", "eq", []string{"1"}}}
		}
		if e.From == "g1" && e.To == "g2" {
			p.Edges[i].Rules = []FieldRule{{"req_type", "ne", []string{"1"}}}
		}
	}
	r := newRig()
	r.s.Org = fakeOrg{}
	r.s.People = func(string) bool { return true }
	_, b := r.board(t)
	b, err := r.s.SaveProcess(ctx, owner, b.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range b.Process.Edges {
		if len(e.Rules) > 0 && e.When != "" {
			t.Errorf("a route kept its expression beside its rules: %+v", e)
		}
	}
	it, err := r.s.StartCase(ctx, owner, b.ID, map[string]any{"subject": "x", "req_type": "1"})
	if err != nil || threadAt(it, "tender").ID == "" {
		t.Fatalf("a tender goes to the tender step: %v %+v", err, it.Flow)
	}
	it, err = r.s.StartCase(ctx, owner, b.ID, map[string]any{"subject": "y", "req_type": "2", "buy_via": []any{"2"}})
	if err != nil || threadAt(it, "online").ID == "" || threadAt(it, "tender").ID != "" {
		t.Fatalf("an inquiry goes on: %v %+v", err, it.Flow)
	}
	bad := miniFlow()
	bad.Edges[1].Rules = []FieldRule{{"req_type", "between", []string{"1"}}}
	if _, err := r.s.SaveProcess(ctx, owner, b.ID, bad); err == nil {
		t.Error("a route with a rule it cannot read was saved")
	}
}
