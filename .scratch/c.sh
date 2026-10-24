cd /Users/jayasurya/work/conveyor-wt/011
git add -A . ':!.scratch'
git commit -q -m "Task 011: Learnings inbox and apply-learnings tasks

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
git push -q -u origin task/011-learnings 2>&1 | tail -2
gh pr create --title "Task 011: Learnings inbox and apply-learnings tasks" --body-file .scratch/body.md --base main
git status --short
