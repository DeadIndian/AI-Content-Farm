# Activate remote verification

`verify-studio.yml` is a ready-to-use GitHub Actions workflow. It installs the application dependencies, runs Go/Python checks, then exercises actual presenter rendering and multi-Shorts ZIP exports through Playwright. No API keys or local neural models are required; CI uses eSpeak.

The credential used to publish this project can push repository code but lacks GitHub's `workflow` scope. GitHub explicitly rejected a push containing `.github/workflows/verify.yml`, so the workflow is stored here as an inactive template.

With an account/token authorized to edit Actions workflows, copy this file to `.github/workflows/verify.yml` and commit it. The next push or pull request starts verification on `ubuntu-latest`. Use the resulting run status to assess remote media verification; this template has not run yet.

```bash
mkdir -p .github/workflows
cp docs/ci/verify-studio.yml .github/workflows/verify.yml
git add .github/workflows/verify.yml
git commit -m "Enable studio integration checks in GitHub Actions"
git push
```
