package notify

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestToastXML(t *testing.T) {
	url := "http://127.0.0.1:7717/?token=ab&x=<y>"
	s, err := toastXML(`kalka: new blueprint from "eve" & co`, "Plan <script>alert(1)</script>\n\nwaits", url)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Launch  string `xml:"launch,attr"`
		Binding struct {
			Template string   `xml:"template,attr"`
			Texts    []string `xml:"text"`
		} `xml:"visual>binding"`
		Actions []struct {
			Content string `xml:"content,attr"`
			Args    string `xml:"arguments,attr"`
		} `xml:"actions>action"`
	}
	if err := xml.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("%v\n%s", err, s)
	}
	if v.Launch != url || v.Binding.Template != "ToastGeneric" || len(v.Binding.Texts) != 2 ||
		!strings.Contains(v.Binding.Texts[1], "<script>") || v.Actions[0].Args != url || v.Actions[1].Args != "dismiss" {
		t.Fatalf("%+v\n%s", v, s)
	}
	if strings.Contains(s, "<script>") {
		t.Fatalf("unescaped: %s", s)
	}
}
