package notify

import (
	"encoding/xml"
	"strings"
)

// toastXML is a Windows toast that stays until dismissed (scenario
// reminder) and opens url on a click or on "Open kalka". Built with
// encoding/xml, so texts and the URL are escaped.
func toastXML(title, message, url string) (string, error) {
	type text struct {
		V string `xml:",chardata"`
	}
	type action struct {
		Content        string `xml:"content,attr"`
		ActivationType string `xml:"activationType,attr"`
		Arguments      string `xml:"arguments,attr"`
	}
	type toast struct {
		XMLName        xml.Name `xml:"toast"`
		ActivationType string   `xml:"activationType,attr"`
		Launch         string   `xml:"launch,attr"`
		Scenario       string   `xml:"scenario,attr"`
		Texts          []text   `xml:"visual>binding>text"`
		Actions        []action `xml:"actions>action"`
	}
	t := toast{
		ActivationType: "protocol", Launch: url, Scenario: "reminder",
		Texts: []text{{title}, {message}},
		Actions: []action{
			{Content: OpenLabel, ActivationType: "protocol", Arguments: url},
			{Content: "Later", ActivationType: "system", Arguments: "dismiss"},
		},
	}
	b, err := xml.Marshal(t)
	if err != nil {
		return "", err
	}
	// encoding/xml cannot put an attribute on the nested <binding>.
	return strings.Replace(string(b), "<binding>", `<binding template="ToastGeneric">`, 1), nil
}
