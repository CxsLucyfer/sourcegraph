package resolvers

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcegraph/log/logtest"

	"github.com/sourcegraph/sourcegraph/enterprise/cmd/frontend/internal/batches/resolvers/apitest"
	"github.com/sourcegraph/sourcegraph/enterprise/internal/batches/store"
	bt "github.com/sourcegraph/sourcegraph/enterprise/internal/batches/testing"
	btypes "github.com/sourcegraph/sourcegraph/enterprise/internal/batches/types"
	"github.com/sourcegraph/sourcegraph/internal/actor"
	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/conf"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/database/dbtest"
	"github.com/sourcegraph/sourcegraph/internal/extsvc/github"
	"github.com/sourcegraph/sourcegraph/internal/observation"
	"github.com/sourcegraph/sourcegraph/internal/timeutil"
	"github.com/sourcegraph/sourcegraph/schema"
)

func TestChangesetResolver(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	logger := logtest.Scoped(t)
	ctx := actor.WithInternalActor(context.Background())
	db := database.NewDB(logger, dbtest.NewDB(logger, t))

	userID := bt.CreateTestUser(t, db, true).ID

	now := timeutil.Now()
	clock := func() time.Time { return now }
	bstore := store.NewWithClock(db, &observation.TestContext, nil, clock)
	esStore := database.ExternalServicesWith(logger, bstore)
	repoStore := database.ReposWith(logger, bstore)

	// Set up the scheduler configuration to a consistent state where a window
	// will always open at 00:00 UTC on the "next" day.
	schedulerWindow := now.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	bt.MockConfig(t, &conf.Unified{
		SiteConfiguration: schema.SiteConfiguration{
			BatchChangesRolloutWindows: &[]*schema.BatchChangeRolloutWindow{
				{
					Rate: "unlimited",
					Days: []string{schedulerWindow.Weekday().String()},
				},
			},
		},
	})

	repo := newGitHubTestRepo("github.com/sourcegraph/changeset-resolver-test", newGitHubExternalService(t, esStore))
	if err := repoStore.Create(ctx, repo); err != nil {
		t.Fatal(err)
	}

	// We use the same mocks for both changesets, even though the unpublished
	// changesets doesn't have a HeadRev (since no commit has been made). The
	// PreviewRepositoryComparison uses a subset of the mocks, though.
	baseRev := "53339e93a17b7934abf3bc4aae3565c15a0631a9"
	headRev := "fa9e174e4847e5f551b31629542377395d6fc95a"
	// These are needed for preview repository comparisons.
	mockBackendCommits(t, api.CommitID(baseRev))
	mockRepoComparison(t, baseRev, headRev, testDiff)

	unpublishedSpec := bt.CreateChangesetSpec(t, ctx, bstore, bt.TestSpecOpts{
		User:          userID,
		Repo:          repo.ID,
		HeadRef:       "refs/heads/my-new-branch",
		Published:     false,
		Title:         "ChangesetSpec Title",
		Body:          "ChangesetSpec Body",
		CommitMessage: "The commit message",
		CommitDiff:    testDiff,
		BaseRev:       baseRev,
		BaseRef:       "refs/heads/master",
		Typ:           btypes.ChangesetSpecTypeBranch,
	})
	unpublishedChangeset := bt.CreateChangeset(t, ctx, bstore, bt.TestChangesetOpts{
		Repo:                repo.ID,
		CurrentSpec:         unpublishedSpec.ID,
		ExternalServiceType: "github",
		PublicationState:    btypes.ChangesetPublicationStateUnpublished,
		ReconcilerState:     btypes.ReconcilerStateCompleted,
	})
	erroredSpec := bt.CreateChangesetSpec(t, ctx, bstore, bt.TestSpecOpts{
		User:          userID,
		Repo:          repo.ID,
		HeadRef:       "refs/heads/my-failing-branch",
		Published:     true,
		Title:         "ChangesetSpec Title",
		Body:          "ChangesetSpec Body",
		CommitMessage: "The commit message",
		CommitDiff:    testDiff,
		BaseRev:       baseRev,
		BaseRef:       "refs/heads/master",
		Typ:           btypes.ChangesetSpecTypeBranch,
	})
	erroredChangeset := bt.CreateChangeset(t, ctx, bstore, bt.TestChangesetOpts{
		Repo:                repo.ID,
		CurrentSpec:         erroredSpec.ID,
		ExternalServiceType: "github",
		PublicationState:    btypes.ChangesetPublicationStateUnpublished,
		ReconcilerState:     btypes.ReconcilerStateErrored,
		FailureMessage:      "very bad error",
	})

	labelEventDescriptionText := "the best label in town"

	syncedGitHubChangeset := bt.CreateChangeset(t, ctx, bstore, bt.TestChangesetOpts{
		Repo:                repo.ID,
		ExternalServiceType: "github",
		ExternalID:          "12345",
		ExternalBranch:      "open-pr",
		ExternalState:       btypes.ChangesetExternalStateOpen,
		ExternalCheckState:  btypes.ChangesetCheckStatePending,
		ExternalReviewState: btypes.ChangesetReviewStateChangesRequested,
		PublicationState:    btypes.ChangesetPublicationStatePublished,
		ReconcilerState:     btypes.ReconcilerStateCompleted,
		Metadata: &github.PullRequest{
			ID:          "12345",
			Title:       "GitHub PR Title",
			Body:        "GitHub PR Body",
			Number:      12345,
			State:       "OPEN",
			URL:         "https://github.com/sourcegraph/sourcegraph/pull/12345",
			HeadRefName: "open-pr",
			HeadRefOid:  headRev,
			BaseRefOid:  baseRev,
			BaseRefName: "master",
			TimelineItems: []github.TimelineItem{
				{Type: "PullRequestCommit", Item: &github.PullRequestCommit{
					Commit: github.Commit{
						OID:           "d34db33f",
						PushedDate:    now,
						CommittedDate: now,
					},
				}},
				{Type: "LabeledEvent", Item: &github.LabelEvent{
					CreatedAt: now.Add(5 * time.Second),
					Label: github.Label{
						ID:          "label-event",
						Name:        "cool-label",
						Color:       "blue",
						Description: labelEventDescriptionText,
					},
				}},
			},
			Labels: struct{ Nodes []github.Label }{
				Nodes: []github.Label{
					{ID: "label-no-description", Name: "no-description", Color: "121212"},
				},
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
	})
	events, err := syncedGitHubChangeset.Events()
	if err != nil {
		t.Fatal(err)
	}
	if err := bstore.UpsertChangesetEvents(ctx, events...); err != nil {
		t.Fatal(err)
	}

	readOnlyGitHubChangeset := bt.CreateChangeset(t, ctx, bstore, bt.TestChangesetOpts{
		Repo:                repo.ID,
		ExternalServiceType: "github",
		ExternalID:          "123456",
		ExternalBranch:      "read-only-pr",
		ExternalState:       btypes.ChangesetExternalStateReadOnly,
		ExternalCheckState:  btypes.ChangesetCheckStatePending,
		ExternalReviewState: btypes.ChangesetReviewStateChangesRequested,
		PublicationState:    btypes.ChangesetPublicationStatePublished,
		ReconcilerState:     btypes.ReconcilerStateCompleted,
		Metadata: &github.PullRequest{
			ID:          "123456",
			Title:       "GitHub PR Title",
			Body:        "GitHub PR Body",
			Number:      123456,
			State:       "OPEN",
			URL:         "https://github.com/sourcegraph/archived/pull/123456",
			HeadRefName: "read-only-pr",
			HeadRefOid:  headRev,
			BaseRefOid:  baseRev,
			BaseRefName: "master",
		},
	})

	unsyncedChangeset := bt.CreateChangeset(t, ctx, bstore, bt.TestChangesetOpts{
		Repo:                repo.ID,
		ExternalServiceType: "github",
		ExternalID:          "9876",
		PublicationState:    btypes.ChangesetPublicationStateUnpublished,
		ReconcilerState:     btypes.ReconcilerStateQueued,
	})

	forkedChangeset := bt.CreateChangeset(t, ctx, bstore, bt.TestChangesetOpts{
		Repo:                  repo.ID,
		ExternalServiceType:   "github",
		ExternalID:            "98765",
		ExternalForkNamespace: "user",
		PublicationState:      btypes.ChangesetPublicationStateUnpublished,
		ReconcilerState:       btypes.ReconcilerStateQueued,
	})

	scheduledChangeset := bt.CreateChangeset(t, ctx, bstore, bt.TestChangesetOpts{
		Repo:                repo.ID,
		ExternalServiceType: "github",
		ExternalID:          "987654",
		PublicationState:    btypes.ChangesetPublicationStateUnpublished,
		ReconcilerState:     btypes.ReconcilerStateScheduled,
	})

	spec := &btypes.BatchSpec{
		UserID:          userID,
		NamespaceUserID: userID,
	}
	if err := bstore.CreateBatchSpec(ctx, spec); err != nil {
		t.Fatal(err)
	}

	batchChange := &btypes.BatchChange{
		Name:            "my-unique-name",
		NamespaceUserID: userID,
		CreatorID:       userID,
		BatchSpecID:     spec.ID,
		LastApplierID:   userID,
		LastAppliedAt:   time.Now(),
	}
	if err := bstore.CreateBatchChange(ctx, batchChange); err != nil {
		t.Fatal(err)
	}
	// Associate the changeset with a batch change, so it's considered in syncer logic.
	addChangeset(t, ctx, bstore, syncedGitHubChangeset, batchChange.ID)

	s, err := newSchema(db, &Resolver{store: bstore})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		changeset *btypes.Changeset
		want      apitest.Changeset
	}{
		{
			name:      "unpublished changeset",
			changeset: unpublishedChangeset,
			want: apitest.Changeset{
				Typename:   "ExternalChangeset",
				Title:      unpublishedSpec.Title,
				Body:       unpublishedSpec.Body,
				Repository: apitest.Repository{Name: string(repo.Name)},
				// Not scheduled for sync, because it's not published.
				NextSyncAt:         "",
				ScheduleEstimateAt: "",
				Labels:             []apitest.Label{},
				Diff: apitest.Comparison{
					Typename:  "PreviewRepositoryComparison",
					FileDiffs: testDiffGraphQL,
				},
				State:       string(btypes.ChangesetStateUnpublished),
				CurrentSpec: apitest.ChangesetSpec{ID: string(marshalChangesetSpecRandID(unpublishedSpec.RandID))},
			},
		},
		{
			name:      "errored changeset",
			changeset: erroredChangeset,
			want: apitest.Changeset{
				Typename:   "ExternalChangeset",
				Title:      erroredSpec.Title,
				Body:       erroredSpec.Body,
				Repository: apitest.Repository{Name: string(repo.Name)},
				// Not scheduled for sync, because it's not published.
				NextSyncAt:         "",
				ScheduleEstimateAt: "",
				Labels:             []apitest.Label{},
				Diff: apitest.Comparison{
					Typename:  "PreviewRepositoryComparison",
					FileDiffs: testDiffGraphQL,
				},
				State:       string(btypes.ChangesetStateRetrying),
				Error:       "very bad error",
				CurrentSpec: apitest.ChangesetSpec{ID: string(marshalChangesetSpecRandID(erroredSpec.RandID))},
			},
		},
		{
			name:      "synced github changeset",
			changeset: syncedGitHubChangeset,
			want: apitest.Changeset{
				Typename:           "ExternalChangeset",
				Title:              "GitHub PR Title",
				Body:               "GitHub PR Body",
				ExternalID:         "12345",
				CheckState:         "PENDING",
				ReviewState:        "CHANGES_REQUESTED",
				NextSyncAt:         marshalDateTime(t, now.Add(8*time.Hour)),
				ScheduleEstimateAt: "",
				Repository:         apitest.Repository{Name: string(repo.Name)},
				ExternalURL: apitest.ExternalURL{
					URL:         "https://github.com/sourcegraph/sourcegraph/pull/12345",
					ServiceKind: "GITHUB",
					ServiceType: "github",
				},
				State: string(btypes.ChangesetStateOpen),
				Events: apitest.ChangesetEventConnection{
					TotalCount: 2,
				},
				Labels: []apitest.Label{
					{Text: "cool-label", Color: "blue", Description: &labelEventDescriptionText},
					{Text: "no-description", Color: "121212", Description: nil},
				},
				Diff: apitest.Comparison{
					Typename:  "RepositoryComparison",
					FileDiffs: testDiffGraphQL,
				},
			},
		},
		{
			name:      "read-only github changeset",
			changeset: readOnlyGitHubChangeset,
			want: apitest.Changeset{
				Typename:           "ExternalChangeset",
				Title:              "GitHub PR Title",
				Body:               "GitHub PR Body",
				ExternalID:         "123456",
				CheckState:         "PENDING",
				ReviewState:        "CHANGES_REQUESTED",
				ScheduleEstimateAt: "",
				Repository:         apitest.Repository{Name: string(repo.Name)},
				ExternalURL: apitest.ExternalURL{
					URL:         "https://github.com/sourcegraph/archived/pull/123456",
					ServiceKind: "GITHUB",
					ServiceType: "github",
				},
				Labels: []apitest.Label{},
				State:  string(btypes.ChangesetStateReadOnly),
			},
		},
		{
			name:      "unsynced changeset",
			changeset: unsyncedChangeset,
			want: apitest.Changeset{
				Typename:   "ExternalChangeset",
				ExternalID: "9876",
				Repository: apitest.Repository{Name: string(repo.Name)},
				Labels:     []apitest.Label{},
				State:      string(btypes.ChangesetStateProcessing),
			},
		},
		{
			name:      "forked changeset",
			changeset: forkedChangeset,
			want: apitest.Changeset{
				Typename:      "ExternalChangeset",
				ExternalID:    "98765",
				ForkNamespace: "user",
				Repository:    apitest.Repository{Name: string(repo.Name)},
				Labels:        []apitest.Label{},
				State:         string(btypes.ChangesetStateProcessing),
			},
		},
		{
			name:      "scheduled changeset",
			changeset: scheduledChangeset,
			want: apitest.Changeset{
				Typename:           "ExternalChangeset",
				ExternalID:         "987654",
				Repository:         apitest.Repository{Name: string(repo.Name)},
				Labels:             []apitest.Label{},
				State:              string(btypes.ChangesetStateScheduled),
				ScheduleEstimateAt: schedulerWindow.Format(time.RFC3339),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			apiID := marshalChangesetID(tc.changeset.ID)
			input := map[string]any{"changeset": apiID}

			var response struct{ Node apitest.Changeset }
			apitest.MustExec(ctx, t, s, input, &response, queryChangeset)

			tc.want.ID = string(apiID)
			if diff := cmp.Diff(tc.want, response.Node); diff != "" {
				t.Fatalf("wrong batch change response (-want +got):\n%s", diff)
			}
		})
	}
}

const queryChangeset = `
fragment fileDiffNode on FileDiff {
    oldPath
    newPath
    oldFile { name }
    hunks {
      body
      oldRange { startLine, lines }
      newRange { startLine, lines }
    }
    stat { added, deleted }
}

query($changeset: ID!) {
  node(id: $changeset) {
    __typename

    ... on ExternalChangeset {
      id

      title
      body

      externalID
      forkNamespace
      state
      reviewState
      checkState
      externalURL { url, serviceKind, serviceType }
      nextSyncAt
      scheduleEstimateAt
      error

      repository { name }

      events(first: 100) { totalCount }
      labels { text, color, description }

      currentSpec { id }

      diff {
        __typename

        ... on RepositoryComparison {
          fileDiffs {
             totalCount
             rawDiff
             diffStat { added, deleted }
             nodes {
               ... fileDiffNode
             }
          }
        }

        ... on PreviewRepositoryComparison {
          fileDiffs {
             totalCount
             rawDiff
             diffStat { added, deleted }
             nodes {
               ... fileDiffNode
             }
          }
        }
      }
    }
  }
}
`
