Sweep every open GitHub PR authored by me and triage its live review comments. This is TRIAGE ONLY: do not change code, commit, push, reply on GitHub or merge. Your final output is the report.

## 1. Find the PRs

    gh search prs --author=@me --state=open --limit 50 --json url,title,repository,number,isDraft

Skip drafts (list them in the report).

## 2. Match each PR to its Conveyor task (never create one)

Find the key (`CV-<n>`, case-insensitive) in the branch, title or body (`gh pr view <n> --repo <owner>/<repo> --json headRefName,title,body`). Otherwise match the PR url against `conveyor task list --json`. Report "untracked" if nothing matches.

## 3. Read the review threads

    gh pr view <n> --repo <owner>/<repo> --json reviews,comments,reviewDecision,mergeable,updatedAt

Inline threads need GraphQL: the REST comments endpoint has no resolution state.

    gh api graphql -f query='query($owner:String!,$repo:String!,$pr:Int!,$cursor:String){repository(owner:$owner,name:$repo){pullRequest(number:$pr){author{login} reviewThreads(first:100,after:$cursor){pageInfo{hasNextPage endCursor} nodes{id isResolved isOutdated path line comments(first:50){totalCount nodes{author{login} createdAt url body}}}}}}}' -f owner=<owner> -f repo=<repo> -F pr=<n>

Page with `-f cursor=<endCursor>` while hasNextPage; if a thread has more than 50 comments, page it too, you need its LAST comment. Classify in order:

- isResolved: drop. It is the only hard done signal.
- unresolved, not outdated, last comment NOT by the PR author: LIVE finding.
- unresolved, last comment by the PR author: answered, drop, unless that reply asks for something back (then live).
- unresolved and outdated: list one line each under "Stale - verify". isOutdated is not evidence of a fix.
- a thread whose only comment is the author's: list under "Author notes".

For each live finding give path:line, who, what is asked and a one-sentence proposed fix. Per PR give a size (trivial / moderate / needs-discussion) and flag anything ambiguous, contradictory between reviewers, or implying a design change instead of proposing a fix.

## 4. Report

Final output: a table of every open PR with its Conveyor task (or untracked), live findings, stale count and size, then the drafts skipped and the untracked PRs. Do not comment on other tasks (a comment requeues a finished task).
