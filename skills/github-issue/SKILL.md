---
name: github-issue
description: Create and manage GitHub issues with proper context and templates. Use when the user asks to file a GitHub issue, report a bug, request a feature, or update/refine an existing issue. Handles template selection, codebase exploration, and GitHub CLI operations.
---

# GitHub Issue Creator

This skill guides the creation and management of high-quality GitHub issues by gathering proper context from the codebase and using appropriate issue templates.

## Workflow Overview

1. **Check for existing issues** - Search for duplicates or related issues first
2. **Check issue templates** - Examine `.github/ISSUE_TEMPLATE/` for available templates
3. **Select best template** - Match user's request to the most appropriate template
4. **Explore the codebase** - Gather context from docs, examples, and relevant code
5. **Create the issue** - Use GitHub CLI (`gh`) to file the issue
6. **Refine if needed** - Edit the issue directly (never comment) for updates

## Step 1: Check for Existing Issues

**ALWAYS** search for existing issues before creating a new one. This prevents duplicates and may reveal that the issue has already been reported or resolved.

### Search for Duplicates

```bash
# Search by keywords from user's query
gh issue list --search "relevant keywords" --state all --limit 15

# Search open issues only
gh issue list --search "error message or feature name" --state open --limit 10

# Search with labels
gh issue list --search "keyword" --label "bug" --limit 10

# View a potentially matching issue
gh issue view <issue-number>
```

### Search Strategies

Extract key terms from the user's query:
- Error messages (exact or partial)
- Feature names or concepts
- Affected components or file names
- Specific behavior descriptions

### When You Find a Matching Issue

**ASK THE USER** before proceeding:

> "I found an existing issue that appears related to your request:
> 
> **#123: [Issue Title]**
> [Brief summary of the issue]
> 
> Would you like me to:
> 1. **Update this existing issue** with additional details
> 2. **Create a new issue** (if this is actually different)
> 
> Please let me know how you'd like to proceed."

### Decision Guide

| Scenario | Recommended Action |
|----------|-------------------|
| Exact same problem/request | Update existing issue |
| Same area, different specifics | Ask user - could be either |
| Clearly different issue | Create new issue, reference related |
| Issue is closed as fixed | Create new issue if regression, reference old |
| Issue is closed as wontfix | Discuss with user before creating new |

If updating an existing issue, skip to **Step 6: Refine and Update Issues**.

## Step 2: Check Issue Templates

Explore the `.github` directory for issue templates:

```bash
# Check for issue templates
ls -la .github/ISSUE_TEMPLATE/ 2>/dev/null || ls -la .github/ 2>/dev/null

# Read each template to understand its purpose
cat .github/ISSUE_TEMPLATE/*.md 2>/dev/null
cat .github/ISSUE_TEMPLATE/*.yml 2>/dev/null
```

Common template types:
- **bug_report** - For reporting bugs, defects, or unexpected behavior
- **feature_request** - For new features or enhancements
- **documentation** - For docs improvements or corrections
- **question** - For questions about usage
- **blank** - Generic template for other issues

If no templates exist, proceed with a standard issue format.

## Step 3: Select the Best Template

Match the user's intent to the appropriate template:

| User Intent | Template |
|-------------|----------|
| "Something is broken", "error", "crash", "doesn't work" | bug_report |
| "Would be nice to have", "add support for", "new feature" | feature_request |
| "Docs are wrong", "unclear documentation", "typo" | documentation |
| "How do I...", "Is it possible to..." | question |
| Other | blank or most general template |

## Step 4: Explore the Codebase

**IMPORTANT**: Err on the side of MORE exploration, especially when:
- This is the start of a new conversation
- The user provided minimal context
- The issue involves specific code behavior
- You need to reference file paths, function names, or configurations

### Exploration Checklist

```bash
# 1. Check documentation
find . -type f -name "*.md" -path "*/docs/*" 2>/dev/null | head -20
find . -type f -name "README*" 2>/dev/null | head -10

# 2. Look at examples
find . -type d -name "examples" 2>/dev/null
find . -type d -name "example" 2>/dev/null

# 3. Search for relevant code based on user's query
# Use grep/ripgrep to find related files
rg -l "relevant_keyword" --type-add 'code:*.{go,py,js,ts,rs}' -t code 2>/dev/null | head -20

# 4. Review existing issues found in Step 1 for additional context
gh issue view <related-issue-number>
```

### What to Gather

- **For bugs**: Relevant source files, expected vs actual behavior, reproduction steps
- **For features**: Related existing code, similar patterns in codebase, affected modules
- **For docs**: Current documentation location, related code it documents

### When to Ask vs Explore

- **Ask the user** if: You need specific reproduction steps, environment details, or preferences
- **Explore yourself** if: You need code context, file locations, existing patterns, or related issues

## Step 5: Create the Issue

Use the GitHub CLI to create the issue:

### With a Template (YAML config)

```bash
# List available templates
gh issue create --help

# Create with specific template
gh issue create \
  --title "Clear, descriptive title" \
  --body "Issue body with all details" \
  --label "bug,priority:high" \
  --template bug_report.yml
```

### Without a Template

```bash
gh issue create \
  --title "Clear, descriptive title" \
  --body "$(cat <<'EOF'
## Description
Brief description of the issue.

## Details
- Relevant context gathered from exploration
- File paths: `path/to/file.go`
- Code references if applicable

## Steps to Reproduce (for bugs)
1. Step one
2. Step two
3. Expected vs actual result

## Additional Context
Any other relevant information.
EOF
)"
```

### Issue Body Best Practices

- Use clear headers (##) for sections
- Include code blocks with syntax highlighting
- Reference specific files with relative paths
- Link to related issues if found: `Related to #123`
- Add labels for categorization
- Assign if you know the appropriate person

## Step 6: Refine and Update Issues

**CRITICAL**: When the user asks to update, refine, or correct an issue:
- **ALWAYS** edit the issue directly using `gh issue edit`
- **NEVER** add a comment - this clutters the issue

### Edit an Existing Issue

```bash
# Edit title
gh issue edit <issue-number> --title "New title"

# Edit body (replaces entire body)
gh issue edit <issue-number> --body "New complete body"

# Edit body from file (useful for complex edits)
gh issue edit <issue-number> --body-file issue_body.md

# Add/remove labels
gh issue edit <issue-number> --add-label "bug" --remove-label "question"

# Add assignees
gh issue edit <issue-number> --add-assignee "@me"
```

### Common Refinement Scenarios

| User Request | Action |
|--------------|--------|
| "Fix the typo in the title" | `gh issue edit <num> --title "..."` |
| "Add more details about X" | Get current body, append details, `gh issue edit <num> --body "..."` |
| "Remove that section" | Get current body, remove section, `gh issue edit <num> --body "..."` |
| "That's wrong, it should be Y" | Get current body, fix the error, `gh issue edit <num> --body "..."` |

### Getting Current Issue Content

```bash
# View issue details
gh issue view <issue-number>

# Get issue body in raw format for editing
gh issue view <issue-number> --json body -q '.body'

# Get full issue data
gh issue view <issue-number> --json title,body,labels,assignees
```

## Quick Reference

### Essential Commands

```bash
# Create issue
gh issue create --title "Title" --body "Body"

# View issue
gh issue view <number>

# Edit issue
gh issue edit <number> --title "New title" --body "New body"

# List issues
gh issue list

# Search issues
gh issue list --search "keyword"

# Close issue
gh issue close <number>

# Reopen issue
gh issue reopen <number>
```

### Labels

Common labels to consider:
- `bug`, `enhancement`, `documentation`
- `good first issue`, `help wanted`
- Priority: `priority:high`, `priority:low`
- Status: `needs-triage`, `wontfix`, `duplicate`

## Troubleshooting

### Authentication

```bash
# Check auth status
gh auth status

# Login if needed
gh auth login
```

### Repository Context

```bash
# Ensure you're in a git repository with remote
git remote -v

# Set repo explicitly if needed
gh issue create --repo owner/repo
```
