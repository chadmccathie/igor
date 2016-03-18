package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/ArjenSchwarz/igor/slack"
)

// StatusPlugin provides status reports for various services
type StatusPlugin struct {
	name        string
	description string
}

// Status instantiates the StatusPlugin
func Status() IgorPlugin {
	plugin := StatusPlugin{
		name:        "status",
		description: "Igor provides status reports for various services",
	}
	return plugin
}

// Work parses the request and ensures a request comes through if any triggers
// are matched. Handled triggers:
func (plugin StatusPlugin) Work(request slack.Request) (slack.Response, error) {
	statuschecks := make(map[string]func() (slack.Attachment, error))
	statuschecks["github"] = plugin.handleGitHubStatus
	statuschecks["bitbucket"] = plugin.handleBitbucketStatus
	statuschecks["npmjs"] = plugin.handleNpmjsStatus
	statuschecks["disqus"] = plugin.handleDisqusStatus
	statuschecks["cloudflare"] = plugin.handleCloudflareStatus
	response := slack.Response{}
	if request.Text == "status" {
		c := make(chan slack.Attachment)
		for _, function := range statuschecks {
			go func(function func() (slack.Attachment, error)) {
				attachment, err := function()
				if err != nil {
					// return response, err
				}
				c <- attachment
			}(function)
		}
		for i := 0; i < len(statuschecks); i++ {
			response.AddAttachment(<-c)
		}
		response.Text = "Status results:"
		response.SetPublic()
	} else if request.Text == "status aws" {
		attachments, _ := plugin.handleAWSStatus()
		for _, attachment := range attachments {
			response.AddAttachment(attachment)
		}
		response.Text = "Status results:"
		response.SetPublic()
	} else if len(request.Text) > 6 && request.Text[:6] == "status" {

		tocheck := request.Text[7:]
		if function, ok := statuschecks[tocheck]; ok {
			// Treat it as a predefined service
			attachment, err := function()
			if err != nil {
				return response, err
			}
			response.AddAttachment(attachment)
			response.Text = "Status results:"
			response.SetPublic()
		} else {
			// Treat it as a website
			attachment, err := plugin.handleDomain(request.Text[7:])
			if err != nil {
				return response, err
			}
			response.AddAttachment(attachment)
			response.Text = "The website is:"
			response.SetPublic()
		}
	}
	if response.Text == "" {
		return response, CreateNoMatchError("Nothing found")
	}
	return response, nil
}

// Describe provides the triggers StatusPlugin can handle
func (StatusPlugin) Describe() map[string]string {
	descriptions := make(map[string]string)
	descriptions["status"] = "Check the status of various services"
	descriptions["status aws"] = "Give an extensive status report on AWS"
	descriptions["status [service]"] = "Check the status of a specific service"
	descriptions["status [url]"] = "Checks if a website is up"
	return descriptions
}

// Description returns a global description of the plugin
func (plugin StatusPlugin) Description() string {
	return plugin.description
}

// Name returns the name of the plugin
func (plugin StatusPlugin) Name() string {
	return plugin.name
}

func (StatusPlugin) handleDomain(domain string) (slack.Attachment, error) {
	attachment := slack.Attachment{Title: domain}
	resp, err := http.Get(fmt.Sprintf("https://isitup.org/%s.json", domain))
	defer resp.Body.Close()
	if err != nil {
		return attachment, err
	}
	var result struct {
		StatusCode int64 `json:"status_code"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return attachment, err
	}
	switch result.StatusCode {
	case 1:
		attachment.Color = "good"
		attachment.Text = ":thumbsup:"
	case 2:
		attachment.Color = "danger"
		attachment.Text = ":thumbsdown:"
	default:
		return attachment, errors.New("Not a valid domain")
	}
	return attachment, nil
}

func (StatusPlugin) handleGitHubStatus() (slack.Attachment, error) {
	attachment := slack.Attachment{Title: "GitHub", PreText: "http://status.github.com"}
	resp, err := http.Get("https://status.github.com/api/last-message.json")
	defer resp.Body.Close()
	if err != nil {
		return attachment, err
	}
	var result struct {
		Status string `json:"status"`
		Body   string `json:"body"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return attachment, err
	}
	attachment.Text = result.Body
	switch result.Status {
	case "good":
		attachment.Color = "good"
	case "minor":
		attachment.Color = "warning"
	case "major":
		attachment.Color = "danger"
	}
	return attachment, nil
}

func (plugin StatusPlugin) handleBitbucketStatus() (slack.Attachment, error) {
	attachment := slack.Attachment{Title: "Bitbucket", PreText: "http://status.bitbucket.org"}
	return plugin.handleStatusPageIo(attachment)
}

func (plugin StatusPlugin) handleNpmjsStatus() (slack.Attachment, error) {
	attachment := slack.Attachment{Title: "NPM", PreText: "http://status.npmjs.org"}
	return plugin.handleStatusPageIo(attachment)
}

func (plugin StatusPlugin) handleDisqusStatus() (slack.Attachment, error) {
	attachment := slack.Attachment{Title: "Disqus", PreText: "http://status.disqus.com"}
	return plugin.handleStatusPageIo(attachment)
}
func (plugin StatusPlugin) handleCloudflareStatus() (slack.Attachment, error) {
	attachment := slack.Attachment{Title: "Cloudflare", PreText: "http://cloudflarestatus.com"}
	return plugin.handleStatusPageIo(attachment)
}

func (plugin StatusPlugin) handleAWSStatus() ([]slack.Attachment, error) {
	attachments := []slack.Attachment{}
	mainAttachment := slack.Attachment{Title: "AWS", PreText: "http://status.aws.amazon.com"}
	attachments = append(attachments, mainAttachment)
	nrResolved := 0
	nrProblems := 0

	doc, err := goquery.NewDocument(mainAttachment.PreText)
	if err != nil {
		return attachments, err
	}
	doc.Find("div#current_events_block table tr").Each(func(i int, s *goquery.Selection) {
		message := strings.Trim(s.Find("td").Eq(2).Text(), " \n")
		if message != "Service is operating normally" && message != "" {
			message = strings.Replace(message, "\n            more \n        \n        \n      ", "\n", 1)
			message = strings.Replace(message, ".", ".\n", -1)
			service := s.Find("td").Eq(1).Text()
			attachment := slack.Attachment{Title: service, Text: message}
			if message[:10] == "[RESOLVED]" {
				attachment.Color = "warning"
				nrResolved++
			} else {
				attachment.Color = "danger"
				nrProblems++
			}
			attachments = append(attachments, attachment)
		}
	})
	if nrProblems != 0 {
		mainAttachment.Color = "danger"
		mainAttachment.Text = fmt.Sprintf("Nr of issues: %s", strconv.Itoa(nrProblems))
		if nrResolved != 0 {
			mainAttachment.Text += fmt.Sprintf("\nNr of resolved issues: %s", strconv.Itoa(nrResolved))
		}
	} else if nrResolved != 0 {
		mainAttachment.Color = "warning"
		mainAttachment.Text = fmt.Sprintf("Nr of resolved issues: %s", strconv.Itoa(nrResolved))
	} else {
		mainAttachment.Color = "good"
		mainAttachment.Text = "Everything is operating normally"
	}
	attachments[0] = mainAttachment

	return attachments, nil
}

func (StatusPlugin) handleStatusPageIo(attachment slack.Attachment) (slack.Attachment, error) {
	doc, err := goquery.NewDocument(attachment.PreText)
	if err != nil {
		return attachment, err
	}
	attachment.Text = doc.Find("div.page-status span.status").Text()
	pageStatus := doc.Find("div.page-status")
	if pageStatus.HasClass("status-none") {
		attachment.Color = "good"
	} else if pageStatus.HasClass("status-yellow") {
		attachment.Color = "warning"
	} else {
		attachment.Color = "danger"
	}
	return attachment, nil
}
