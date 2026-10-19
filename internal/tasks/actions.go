package tasks

// Step actions: what a process does by itself when a step opens (Node.Before)
// and when it is done (Node.After) -- what BPM tools call triggers, as a list
// to choose from rather than code to write. Each action may carry
// rules (If, Any) and is skipped when they do not hold.
//
//	set      a question of the case takes a value: "=formula" over the
//	         case's answers (calc.go), or plain text
//	notify   people are told, in docfind's notification centre: To is whom,
//	         as a step's assignment says it; Text may name answers as {key}
//	webhook  the case is posted to a web address, through the same door as a
//	         board's webhooks (so the same hosts are allowed)
//
// A "set" changes the case at once, so the routes and the next steps see it;
// notices and posts go out once the case is saved.

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// StepAction is one thing a step does by itself.
type StepAction struct {
	Kind    string      `json:"kind"`
	Field   string      `json:"field,omitempty"`
	Formula string      `json:"formula,omitempty"`
	To      *Assign     `json:"to,omitempty"`
	Text    string      `json:"text,omitempty"`
	URL     string      `json:"url,omitempty"`
	If      []FieldRule `json:"if,omitempty"`
	Any     bool        `json:"any,omitempty"`
}

var stepActionKinds = []string{"set", "notify", "webhook"}

// cleanStepActions checks a step's actions.
func cleanStepActions(in []StepAction, where string) ([]StepAction, error) {
	if len(in) > 20 {
		return nil, Invalid{"«" + where + "»: بیش از ۲۰ اقدام."}
	}
	var out []StepAction
	for _, a := range in {
		bad := func(what string) error { return Invalid{"«" + where + "»، اقدام: " + what} }
		if !slices.Contains(stepActionKinds, a.Kind) {
			return nil, bad("نوعش شناخته نیست.")
		}
		rules, err := cleanRules(a.If, "")
		if err != nil {
			return nil, bad(err.Error())
		}
		a.If, a.Any = rules, a.Any && len(rules) > 1
		switch a.Kind {
		case "set":
			if a.Field = strings.TrimSpace(a.Field); !fieldKey.MatchString(a.Field) {
				return nil, bad("کدام پرسش را پر کند؟")
			}
			if a.Formula = strings.TrimSpace(a.Formula); strings.HasPrefix(a.Formula, "=") {
				if a.Formula, err = cleanFormula(a.Formula); err != nil {
					return nil, bad(err.Error())
				}
			} else if len(a.Formula) > 500 {
				a.Formula = a.Formula[:500]
			}
			a.To, a.Text, a.URL = nil, "", ""
		case "notify":
			if a.To == nil || !slices.Contains(assignModes, a.To.Mode) {
				return nil, bad("به چه کسی گفته شود؟")
			}
			to := *a.To
			if to.Users, err = users(to.Users, 50); err != nil {
				return nil, err
			}
			to.Field, to.Group = strings.TrimSpace(to.Field), ""
			a.To = &to
			if a.Text = clipText(a.Text, 1000); a.Text == "" {
				return nil, bad("پیامی ندارد.")
			}
			a.Field, a.Formula, a.URL = "", "", ""
		case "webhook":
			u, err := url.Parse(strings.TrimSpace(a.URL))
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(a.URL) > 500 {
				return nil, bad("نشانی وب درستی نیست.")
			}
			a.URL = u.String()
			a.Field, a.Formula, a.To, a.Text = "", "", nil, ""
		}
		out = append(out, a)
	}
	return out, nil
}

// effect is a notice or a post, sent once the case is saved.
type effect struct {
	notify []string
	title  string
	text   string
	url    string
	event  string
	node   string
}

// act does a step's actions: the "set"s at once on the case's data, the rest
// kept in r.effects for after the save.
func (r *router) act(n Node, when string, actions []StepAction) {
	for _, a := range actions {
		if len(a.If) > 0 && !rulesMatch(a.If, a.Any, r.f.Data) {
			continue
		}
		switch a.Kind {
		case "set":
			if v := valueOf(a.Formula, r.f.Data); v != nil {
				r.f.Data[a.Field] = v
			}
		case "notify":
			who, pool := r.assign(*a.To)
			r.effects = append(r.effects, effect{notify: append(who, pool...), title: n.Name, text: fillText(a.Text, r.f.Data)})
		case "webhook":
			r.effects = append(r.effects, effect{url: a.URL, event: "step." + when, node: n.ID})
		}
	}
}

var answerRef = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]{0,63})\}`)

// fillText puts a case's answers into a message where it names them: {key}.
func fillText(text string, data map[string]any) string {
	return answerRef.ReplaceAllStringFunc(text, func(m string) string {
		v, ok := data[m[1:len(m)-1]]
		if !ok || blank(v) {
			return "—"
		}
		if n, ok := v.(float64); ok {
			return numText(n)
		}
		return oneLine(asString(v), 200)
	})
}

// sendEffects sends what a step's actions left for after the save.
func (s *Service) sendEffects(ctx context.Context, it Item, b Board, effects []effect, actor string) {
	for _, e := range effects {
		if e.url != "" {
			if s.Hook == nil {
				continue
			}
			body, err := json.Marshal(map[string]any{"event": e.event, "at": s.now(), "step": e.node,
				"board": map[string]string{"id": b.ID, "name": b.Name}, "item": it})
			if err == nil {
				s.Hook(ctx, Webhook{URL: e.url, Events: []string{e.event}}, e.event, body)
			}
			continue
		}
		s.tell(ctx, e.notify, actor, "«"+e.title+"»: "+it.Title, e.text, ItemLink(it))
	}
}
