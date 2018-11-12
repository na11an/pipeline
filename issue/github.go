// Copyright © 2018 Banzai Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package issue

import (
	"context"
	"fmt"

	"github.com/banzaicloud/pipeline/auth"
	"github.com/google/go-github/github"
	"github.com/spf13/viper"
)

// GitHubIssuer creates an issue on GitHub
type GitHubIssuer struct {
	Version VersionInformation
}

var _ Issuer = GitHubIssuer{}

const issueTemplate = `
**Version Information:**
%s

**User Information:**
ID: %d
Organization: %s

**Description:**
%s`

// CreateIssue creates an issue on GitHub
func (gi GitHubIssuer) CreateIssue(userID uint, organization, title, text string) error {

	githubClient := auth.NewGithubClient(viper.GetString("github.token"))

	body := fmt.Sprintf(issueTemplate, gi.Version, userID, organization, text)

	labels := viper.GetStringSlice("issue.githubLabels")
	issue := github.IssueRequest{
		Title:  github.String(title),
		Body:   github.String(body),
		Labels: &labels,
	}

	_, _, err := githubClient.Issues.Create(
		context.TODO(),
		viper.GetString("issue.githubOwner"),
		viper.GetString("issue.githubRepository"),
		&issue,
	)

	return err
}