// Copyright 2021 Allstar Authors

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package branch

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v43/github"
	"github.com/ossf/allstar/pkg/config"
	"github.com/ossf/allstar/pkg/policydef"
)

var get func(context.Context, string, string) (*github.Repository,
	*github.Response, error)
var listBranches func(context.Context, string, string,
	*github.BranchListOptions) ([]*github.Branch, *github.Response, error)
var getBranchProtection func(context.Context, string, string, string) (
	*github.Protection, *github.Response, error)
var updateBranchProtection func(context.Context, string, string, string,
	*github.ProtectionRequest) (*github.Protection, *github.Response, error)

type mockRepos struct{}

func (m mockRepos) Get(ctx context.Context, o string, r string) (
	*github.Repository, *github.Response, error) {
	return get(ctx, o, r)
}

func (m mockRepos) ListBranches(ctx context.Context, o string, r string,
	op *github.BranchListOptions) ([]*github.Branch, *github.Response, error) {
	return listBranches(ctx, o, r, op)
}

func (m mockRepos) GetBranchProtection(ctx context.Context, o string, r string,
	b string) (*github.Protection, *github.Response, error) {
	return getBranchProtection(ctx, o, r, b)
}

func (m mockRepos) UpdateBranchProtection(ctx context.Context, owner, repo,
	branch string, preq *github.ProtectionRequest) (*github.Protection,
	*github.Response, error) {
	return updateBranchProtection(ctx, owner, repo, branch, preq)
}

func TestConfigPrecedence(t *testing.T) {
	tests := []struct {
		Name      string
		Org       OrgConfig
		OrgRepo   RepoConfig
		Repo      RepoConfig
		ExpAction string
		Exp       mergedConfig
	}{
		{
			Name: "OrgOnly",
			Org: OrgConfig{
				Action:                "issue",
				EnforceDefault:        true,
				RequireUpToDateBranch: true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
			OrgRepo:   RepoConfig{},
			Repo:      RepoConfig{},
			ExpAction: "issue",
			Exp: mergedConfig{
				Action:                "issue",
				EnforceDefault:        true,
				RequireUpToDateBranch: true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
		},
		{
			Name: "OrgRepoOverOrg",
			Org: OrgConfig{
				Action:                "issue",
				EnforceDefault:        true,
				RequireUpToDateBranch: true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
			OrgRepo: RepoConfig{
				Action: github.String("log"),
				RequireStatusChecks: []StatusCheck{
					{"someothercheck", nil},
				},
			},
			Repo:      RepoConfig{},
			ExpAction: "log",
			Exp: mergedConfig{
				Action:                "log",
				EnforceDefault:        true,
				RequireUpToDateBranch: true,
				RequireStatusChecks: []StatusCheck{
					{"someothercheck", nil},
				},
			},
		},
		{
			Name: "RepoOverAllOrg",
			Org: OrgConfig{
				Action:                "issue",
				EnforceDefault:        true,
				RequireUpToDateBranch: true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
			OrgRepo: RepoConfig{
				Action: github.String("log"),
				RequireStatusChecks: []StatusCheck{
					{"someothercheck", nil},
				},
			},
			Repo: RepoConfig{
				Action: github.String("email"),
				RequireStatusChecks: []StatusCheck{
					{"bestcheck", nil},
				},
			},
			ExpAction: "email",
			Exp: mergedConfig{
				Action:                "email",
				EnforceDefault:        true,
				RequireUpToDateBranch: true,
				RequireStatusChecks: []StatusCheck{
					{"bestcheck", nil},
				},
			},
		},
		{
			Name: "RepoDisallowed",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					DisableRepoOverride: true,
				},
				Action:                "issue",
				EnforceDefault:        true,
				RequireUpToDateBranch: true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
			OrgRepo: RepoConfig{
				Action: github.String("log"),
				RequireStatusChecks: []StatusCheck{
					{"someothercheck", nil},
				},
			},
			Repo: RepoConfig{
				Action: github.String("email"),
				RequireStatusChecks: []StatusCheck{
					{"bestcheck", nil},
				},
			},
			ExpAction: "log",
			Exp: mergedConfig{
				Action:                "log",
				EnforceDefault:        true,
				RequireUpToDateBranch: true,
				RequireStatusChecks: []StatusCheck{
					{"someothercheck", nil},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			configFetchConfig = func(ctx context.Context, c *github.Client,
				owner, repo, path string, ol config.ConfigLevel, out interface{}) error {
				switch ol {
				case config.RepoLevel:
					rc := out.(*RepoConfig)
					*rc = test.Repo
				case config.OrgRepoLevel:
					orc := out.(*RepoConfig)
					*orc = test.OrgRepo
				case config.OrgLevel:
					oc := out.(*OrgConfig)
					*oc = test.Org
				}
				return nil
			}

			b := Branch(true)
			ctx := context.Background()

			action := b.GetAction(ctx, nil, "", "thisrepo")
			if action != test.ExpAction {
				t.Errorf("Unexpected results. want %s, got %s", test.ExpAction, action)
			}

			oc, orc, rc := getConfig(ctx, nil, "", "thisrepo")
			mc := mergeConfig(oc, orc, rc, "thisrepo")
			if diff := cmp.Diff(&test.Exp, mc); diff != "" {
				t.Errorf("Unexpected results. (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		Name              string
		Org               OrgConfig
		Repo              RepoConfig
		Prot              map[string]github.Protection
		cofigEnabled      bool
		doNothingOnOptOut bool
		Exp               policydef.Result
	}{
		{
			Name: "NotEnabled",
			Org: OrgConfig{
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   1,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 1,
					},
				},
			},
			cofigEnabled:      false,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    false,
				Pass:       true,
				NotifyText: "",
				Details: map[string]details{
					"main": details{
						PRReviews:    true,
						NumReviews:   1,
						DismissStale: true,
						BlockForce:   true,
					},
				},
			},
		},
		{
			Name: "NotEnabledDoNothing",
			Org: OrgConfig{
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   1,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 1,
					},
				},
			},
			cofigEnabled:      false,
			doNothingOnOptOut: true,
			Exp: policydef.Result{
				Enabled:    false,
				Pass:       true,
				NotifyText: "Disabled",
				Details:    map[string]details{},
			},
		},
		{
			Name: "CatchBlockForce",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   1,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 5,
					},
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: true,
					},
				},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       false,
				NotifyText: "Block force push not configured for branch main\n",
				Details: map[string]details{
					"main": details{
						PRReviews:    true,
						NumReviews:   5,
						DismissStale: true,
						BlockForce:   false,
					},
				},
			},
		},
		{
			Name: "CatchReleaseBranch",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   1,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo: RepoConfig{
				EnforceBranches: []string{"release"},
			},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 2,
					},
				},
				"release": github.Protection{},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       false,
				NotifyText: "PR Approvals not configured for branch release\n",
				Details: map[string]details{
					"main": details{
						PRReviews:    true,
						NumReviews:   2,
						DismissStale: true,
						BlockForce:   true,
					},
					"release": details{
						PRReviews:    false,
						NumReviews:   0,
						DismissStale: false,
						BlockForce:   true,
					},
				},
			},
		},
		{
			Name: "CatchRequireUpToDateBranchNoConfig",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:        true,
				RequireApproval:       true,
				ApprovalCount:         1,
				DismissStale:          true,
				BlockForce:            true,
				RequireUpToDateBranch: true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 5,
					},
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
				},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       true,
				NotifyText: "",
				Details: map[string]details{
					"main": details{
						PRReviews:             true,
						NumReviews:            5,
						DismissStale:          true,
						BlockForce:            true,
						RequireUpToDateBranch: false,
					},
				},
			},
		},
		{
			Name: "CatchRequireUpToDateBranchStrictFalse",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:        true,
				RequireApproval:       true,
				ApprovalCount:         1,
				DismissStale:          true,
				BlockForce:            true,
				RequireUpToDateBranch: true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 5,
					},
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: false,
						Checks: []*github.RequiredStatusCheck{
							{Context: "mycheck"}, {Context: "theothercheck"},
						},
					},
				},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       false,
				NotifyText: "Require up to date branch not configured for branch main\n",
				Details: map[string]details{
					"main": details{
						PRReviews:             true,
						NumReviews:            5,
						DismissStale:          true,
						BlockForce:            true,
						RequireUpToDateBranch: false,
						RequireStatusChecks: []StatusCheck{
							{"mycheck", nil}, {"theothercheck", nil},
						},
					},
				},
			},
		},
		{
			Name: "CatchRequireStatusChecksNoConfig",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   1,
				DismissStale:    true,
				BlockForce:      true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 5,
					},
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
				},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       false,
				NotifyText: "Status checks required by policy, but none found for branch main\n",
				Details: map[string]details{
					"main": details{
						PRReviews:    true,
						NumReviews:   5,
						DismissStale: true,
						BlockForce:   true,
					},
				},
			},
		},
		{
			Name: "CatchRequireStatusChecks",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   1,
				DismissStale:    true,
				BlockForce:      true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 5,
					},
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: false,
						Checks: []*github.RequiredStatusCheck{
							{Context: "mycheck"},
						},
					},
				},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       false,
				NotifyText: "Status check theothercheck (any app) not found for branch main\n",
				Details: map[string]details{
					"main": details{
						PRReviews:           true,
						NumReviews:          5,
						DismissStale:        true,
						BlockForce:          true,
						RequireStatusChecks: []StatusCheck{{"mycheck", nil}},
					},
				},
			},
		},
		{
			Name: "CatchRequireStatusChecksNilAppID",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:      true,
				RequireApproval:     true,
				ApprovalCount:       1,
				DismissStale:        true,
				BlockForce:          true,
				RequireStatusChecks: []StatusCheck{{"mycheck", nil}},
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 5,
					},
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: false,
						Checks: []*github.RequiredStatusCheck{
							{Context: "mycheck", AppID: github.Int64(123456)},
						},
					},
				},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       true,
				NotifyText: "",
				Details: map[string]details{
					"main": details{
						PRReviews:           true,
						NumReviews:          5,
						DismissStale:        true,
						BlockForce:          true,
						RequireStatusChecks: []StatusCheck{{"mycheck", github.Int64(123456)}},
					},
				},
			},
		},
		{
			Name: "CatchRequireStatusChecksWrongAppID",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:      true,
				RequireApproval:     true,
				ApprovalCount:       1,
				DismissStale:        true,
				BlockForce:          true,
				RequireStatusChecks: []StatusCheck{{"mycheck", github.Int64(123456)}},
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 5,
					},
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: false,
						Checks: []*github.RequiredStatusCheck{
							{Context: "mycheck", AppID: github.Int64(654321)},
						},
					},
				},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       false,
				NotifyText: "Status check mycheck (AppID: 123456) not found for branch main\n",
				Details: map[string]details{
					"main": details{
						PRReviews:           true,
						NumReviews:          5,
						DismissStale:        true,
						BlockForce:          true,
						RequireStatusChecks: []StatusCheck{{"mycheck", github.Int64(654321)}},
					},
				},
			},
		},
		{
			Name: "CatchEnforceAdminsAdminEnforcementNil",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   1,
				DismissStale:    true,
				BlockForce:      true,
				EnforceOnAdmins: true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 5,
					},
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
				},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       false,
				NotifyText: "Enforce status checks on admins not configured for branch main\n",
				Details: map[string]details{
					"main": details{
						PRReviews:             true,
						NumReviews:            5,
						DismissStale:          true,
						BlockForce:            true,
						RequireUpToDateBranch: false,
						EnforceOnAdmins:       false,
					},
				},
			},
		},
		{
			Name: "CatchEnforceAdminsAdminEnforcementDisabled",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   1,
				DismissStale:    true,
				BlockForce:      true,
				EnforceOnAdmins: true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 5,
					},
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
				},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       false,
				NotifyText: "Enforce status checks on admins not configured for branch main\n",
				Details: map[string]details{
					"main": details{
						PRReviews:             true,
						NumReviews:            5,
						DismissStale:          true,
						BlockForce:            true,
						RequireUpToDateBranch: false,
						EnforceOnAdmins:       false,
					},
				},
			},
		},
		{
			Name: "RepoOverride",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   2,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo: RepoConfig{
				ApprovalCount: github.Int(1),
				DismissStale:  github.Bool(false),
			},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          false,
						RequiredApprovingReviewCount: 1,
					},
				},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       true,
				NotifyText: "",
				Details: map[string]details{
					"main": details{
						PRReviews:    true,
						NumReviews:   1,
						DismissStale: false,
						BlockForce:   true,
					},
				},
			},
		},
		{
			Name: "RepoOverridePrevented",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy:      true,
					DisableRepoOverride: true,
				},
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   2,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo: RepoConfig{
				ApprovalCount: github.Int(1),
				DismissStale:  github.Bool(false),
			},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          false,
						RequiredApprovingReviewCount: 1,
					},
				},
			},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       false,
				NotifyText: "Dismiss stale reviews not configured for branch main\nPR Approvals below threshold 1 : 2 for branch main\n",
				Details: map[string]details{
					"main": details{
						PRReviews:    true,
						NumReviews:   1,
						DismissStale: false,
						BlockForce:   true,
					},
				},
			},
		},
		{
			Name: "NoProtection",
			Org: OrgConfig{
				OptConfig: config.OrgOptConfig{
					OptOutStrategy: true,
				},
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   2,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo:              RepoConfig{},
			Prot:              map[string]github.Protection{},
			cofigEnabled:      true,
			doNothingOnOptOut: false,
			Exp: policydef.Result{
				Enabled:    true,
				Pass:       false,
				NotifyText: "No protection found for branch main\n",
				Details: map[string]details{
					"main": details{
						PRReviews:    false,
						NumReviews:   0,
						DismissStale: false,
						BlockForce:   false,
					},
				},
			},
		},
	}

	get = func(context.Context, string, string) (*github.Repository,
		*github.Response, error) {
		b := "main"
		return &github.Repository{
			DefaultBranch: &b,
		}, nil, nil
	}
	listBranches = func(context.Context, string, string,
		*github.BranchListOptions) ([]*github.Branch, *github.Response, error) {
		return []*github.Branch{
			&github.Branch{},
		}, &github.Response{NextPage: 0}, nil
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			configFetchConfig = func(ctx context.Context, c *github.Client,
				owner, repo, path string, ol config.ConfigLevel, out interface{}) error {
				if repo == "thisrepo" && ol == config.RepoLevel {
					rc := out.(*RepoConfig)
					*rc = test.Repo
				} else if ol == config.OrgLevel {
					oc := out.(*OrgConfig)
					*oc = test.Org
				}
				return nil
			}
			getBranchProtection = func(ctx context.Context, o string, r string,
				b string) (*github.Protection, *github.Response, error) {
				p, ok := test.Prot[b]
				if ok {
					return &p, nil, nil
				} else {
					return nil, &github.Response{
						Response: &http.Response{
							StatusCode: http.StatusNotFound,
						},
					}, errors.New("404")
				}
			}
			configIsEnabled = func(ctx context.Context, o config.OrgOptConfig, orc, r config.RepoOptConfig,
				c *github.Client, owner, repo string) (bool, error) {
				return test.cofigEnabled, nil
			}
			doNothingOnOptOut = test.doNothingOnOptOut
			res, err := check(context.Background(), mockRepos{}, nil, "", "thisrepo")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if diff := cmp.Diff(&test.Exp, res); diff != "" {
				t.Errorf("Unexpected results. (-want +got):\n%s", diff)
			}
		})
	}
	t.Run("Emptyrepo", func(t *testing.T) {
		listBranches = func(context.Context, string, string,
			*github.BranchListOptions) ([]*github.Branch, *github.Response, error) {
			return []*github.Branch{}, &github.Response{NextPage: 0}, nil
		}
		res, err := check(context.Background(), mockRepos{}, nil, "", "thisrepo")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		expect := &policydef.Result{
			Enabled:    true,
			Pass:       true,
			NotifyText: "No branches to protect",
		}
		if diff := cmp.Diff(expect, res); diff != "" {
			t.Errorf("Unexpected results. (-want +got):\n%s", diff)
		}
	})
}

func TestFix(t *testing.T) {
	tests := []struct {
		Name         string
		Org          OrgConfig
		Repo         RepoConfig
		Prot         map[string]github.Protection
		cofigEnabled bool
		Exp          map[string]github.ProtectionRequest
	}{
		{
			Name: "NoChange",
			Org: OrgConfig{
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   2,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 2,
					},
				},
			},
			cofigEnabled: true,
			Exp:          map[string]github.ProtectionRequest{},
		},
		{
			Name: "AddProtection",
			Org: OrgConfig{
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   2,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: nil,
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict:   true,
						Contexts: []string{"mycheck"},
						Checks: []*github.RequiredStatusCheck{
							&github.RequiredStatusCheck{
								Context: "mycheck",
								AppID:   github.Int64(123),
							},
						},
					},
				},
			},
			cofigEnabled: true,
			Exp: map[string]github.ProtectionRequest{
				"main": github.ProtectionRequest{
					AllowForcePushes: github.Bool(false),
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 2,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: true,
						Checks: []*github.RequiredStatusCheck{ // No Contexts in request
							&github.RequiredStatusCheck{
								Context: "mycheck",
								AppID:   github.Int64(123),
							},
						},
					},
				},
			},
		},
		{
			Name: "AddProtectionFromScratch",
			Org: OrgConfig{
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   2,
				DismissStale:    true,
				BlockForce:      true,
				EnforceOnAdmins: true,
			},
			Repo:         RepoConfig{},
			Prot:         map[string]github.Protection{},
			cofigEnabled: true,
			Exp: map[string]github.ProtectionRequest{
				"main": github.ProtectionRequest{
					EnforceAdmins:    true,
					AllowForcePushes: github.Bool(false),
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 2,
					},
				},
			},
		},
		{
			Name: "NotEnabled",
			Org: OrgConfig{
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   2,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: nil,
				},
			},
			cofigEnabled: false,
			Exp:          map[string]github.ProtectionRequest{},
		},
		{
			Name: "IncreaseCountAndBlockForce",
			Org: OrgConfig{
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   2,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: true,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 1,
					},
				},
			},
			cofigEnabled: true,
			Exp: map[string]github.ProtectionRequest{
				"main": github.ProtectionRequest{
					AllowForcePushes: github.Bool(false),
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 2,
					},
				},
			},
		},
		{
			Name: "BlockForceOnly",
			Org: OrgConfig{
				EnforceDefault: true,
				BlockForce:     true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: true,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						RequiredApprovingReviewCount: 0,
					},
				},
			},
			cofigEnabled: true,
			Exp: map[string]github.ProtectionRequest{
				"main": github.ProtectionRequest{
					AllowForcePushes: github.Bool(false),
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
						RequiredApprovingReviewCount: 0,
					},
				},
			},
		},
		{
			Name: "RequireUpToDateBranchOnly",
			Org: OrgConfig{
				EnforceDefault:        true,
				RequireUpToDateBranch: true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						RequiredApprovingReviewCount: 0,
					},
				},
			},
			cofigEnabled: true,
			Exp:          map[string]github.ProtectionRequest{},
		},
		{
			Name: "RequireUpToDateBranch",
			Org: OrgConfig{
				EnforceDefault:        true,
				RequireUpToDateBranch: true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						RequiredApprovingReviewCount: 0,
					},
				},
			},
			cofigEnabled: true,
			Exp: map[string]github.ProtectionRequest{
				"main": github.ProtectionRequest{
					AllowForcePushes: github.Bool(false),
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
						RequiredApprovingReviewCount: 0,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: true,
						Checks: []*github.RequiredStatusCheck{
							{Context: "mycheck"}, {Context: "theothercheck"},
						},
					},
				},
			},
		},
		{
			Name: "EnforceAdmins",
			Org: OrgConfig{
				EnforceDefault:  true,
				EnforceOnAdmins: true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						RequiredApprovingReviewCount: 0,
					},
				},
			},
			cofigEnabled: true,
			Exp: map[string]github.ProtectionRequest{
				"main": github.ProtectionRequest{
					AllowForcePushes: github.Bool(false),
					EnforceAdmins:    true,
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
						RequiredApprovingReviewCount: 0,
					},
				},
			},
		},
		{
			Name: "RequireStatusChecksOnly",
			Org: OrgConfig{
				EnforceDefault: true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						RequiredApprovingReviewCount: 0,
					},
				},
			},
			cofigEnabled: true,
			Exp: map[string]github.ProtectionRequest{
				"main": github.ProtectionRequest{
					AllowForcePushes: github.Bool(false),
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
						RequiredApprovingReviewCount: 0,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: false,
						Checks: []*github.RequiredStatusCheck{
							{Context: "mycheck"}, {Context: "theothercheck"},
						},
					},
				},
			},
		},
		{
			Name: "MergeRequireStatusChecks",
			Org: OrgConfig{
				EnforceDefault: true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						RequiredApprovingReviewCount: 0,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: false,
						Checks: []*github.RequiredStatusCheck{
							{Context: "mycheck"}, {Context: "someothercheck"},
						},
					},
				},
			},
			cofigEnabled: true,
			Exp: map[string]github.ProtectionRequest{
				"main": github.ProtectionRequest{
					AllowForcePushes: github.Bool(false),
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
						RequiredApprovingReviewCount: 0,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: false,
						Checks: []*github.RequiredStatusCheck{
							{Context: "mycheck"}, {Context: "someothercheck"}, {Context: "theothercheck"},
						},
					},
				},
			},
		},
		{
			Name: "MergeRequireStatusChecksDifferentAppID",
			Org: OrgConfig{
				EnforceDefault: true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", github.Int64(123456)}, {"theothercheck", nil},
					{"someothercheck", github.Int64(654321)},
				},
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						RequiredApprovingReviewCount: 0,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: false,
						Checks: []*github.RequiredStatusCheck{
							{Context: "mycheck"},
							{Context: "someothercheck", AppID: github.Int64(123456)},
						},
					},
				},
			},
			cofigEnabled: true,
			Exp: map[string]github.ProtectionRequest{
				"main": github.ProtectionRequest{
					AllowForcePushes: github.Bool(false),
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
						RequiredApprovingReviewCount: 0,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: false,
						Checks: []*github.RequiredStatusCheck{
							{Context: "mycheck"},
							{Context: "mycheck", AppID: github.Int64(123456)},
							{Context: "someothercheck", AppID: github.Int64(123456)},
							{Context: "someothercheck", AppID: github.Int64(654321)},
							{Context: "theothercheck"},
						},
					},
				},
			},
		},
		{
			Name: "NoChangeToRequireStatusChecks",
			Org: OrgConfig{
				EnforceDefault: true,
				RequireStatusChecks: []StatusCheck{
					{"mycheck", nil}, {"theothercheck", nil},
				},
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						RequiredApprovingReviewCount: 0,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: false,
						Checks: []*github.RequiredStatusCheck{
							{Context: "mycheck"}, {Context: "theothercheck"},
						},
					},
				},
			},
			cofigEnabled: true,
			Exp:          map[string]github.ProtectionRequest{},
		},
		{
			Name: "HandleExistingEmptyChecks",
			Org: OrgConfig{
				EnforceDefault:  true,
				RequireApproval: true,
				ApprovalCount:   1,
				DismissStale:    true,
				BlockForce:      true,
			},
			Repo: RepoConfig{},
			Prot: map[string]github.Protection{
				"main": github.Protection{
					AllowForcePushes: &github.AllowForcePushes{
						Enabled: false,
					},
					EnforceAdmins: &github.AdminEnforcement{
						Enabled: false,
					},
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcement{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 1,
					},
					RequiredStatusChecks: &github.RequiredStatusChecks{
						Strict: false,
					},
				},
			},
			cofigEnabled: true,
			Exp: map[string]github.ProtectionRequest{
				"main": github.ProtectionRequest{
					AllowForcePushes: github.Bool(false),
					RequiredPullRequestReviews: &github.PullRequestReviewsEnforcementRequest{
						DismissStaleReviews:          true,
						RequiredApprovingReviewCount: 1,
					},
					RequiredStatusChecks: nil,
				},
			},
		},
	}
	get = func(context.Context, string, string) (*github.Repository,
		*github.Response, error) {
		b := "main"
		return &github.Repository{
			DefaultBranch: &b,
		}, nil, nil
	}
	listBranches = func(context.Context, string, string,
		*github.BranchListOptions) ([]*github.Branch, *github.Response, error) {
		return []*github.Branch{
			&github.Branch{},
		}, &github.Response{NextPage: 0}, nil
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			got := make(map[string]github.ProtectionRequest)
			updateBranchProtection = func(ctx context.Context, owner, repo,
				branch string, preq *github.ProtectionRequest) (*github.Protection,
				*github.Response, error) {
				got[branch] = *preq
				return nil, nil, nil
			}
			configFetchConfig = func(ctx context.Context, c *github.Client,
				owner, repo, path string, ol config.ConfigLevel, out interface{}) error {
				if repo == "thisrepo" && ol == config.RepoLevel {
					rc := out.(*RepoConfig)
					*rc = test.Repo
				} else if ol == config.OrgLevel {
					oc := out.(*OrgConfig)
					*oc = test.Org
				}
				return nil
			}
			getBranchProtection = func(ctx context.Context, o string, r string,
				b string) (*github.Protection, *github.Response, error) {
				p, ok := test.Prot[b]
				if ok {
					return &p, nil, nil
				} else {
					return nil, &github.Response{
						Response: &http.Response{
							StatusCode: http.StatusNotFound,
						},
					}, errors.New("404")
				}
			}
			configIsEnabled = func(ctx context.Context, o config.OrgOptConfig, orc, r config.RepoOptConfig,
				c *github.Client, owner, repo string) (bool, error) {
				return test.cofigEnabled, nil
			}
			if err := fix(context.Background(), mockRepos{}, nil, "", "thisrepo"); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Sort required status checks by context to ensure comparison is consistent.
			for _, pr := range got {
				if pr.RequiredStatusChecks != nil {
					sc := make([]*github.RequiredStatusCheck, 0)
					cm := make(map[string][]*github.RequiredStatusCheck)
					for _, check := range pr.RequiredStatusChecks.Checks {
						cm[check.Context] = append(cm[check.Context], check)
					}
					ctx := make([]string, 0)
					for c := range cm {
						ctx = append(ctx, c)
					}
					sort.Strings(ctx)
					for _, c := range ctx {
						sc = append(sc, cm[c]...)
					}
					pr.RequiredStatusChecks.Checks = sc
				}
			}

			if diff := cmp.Diff(test.Exp, got); diff != "" {
				t.Errorf("Unexpected results. (-want +got):\n%s", diff)
			}
		})
	}

}
