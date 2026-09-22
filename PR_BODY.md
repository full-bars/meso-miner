## What

Follow-up to `#99`: a tag push made with `GITHUB_TOKEN` does not trigger other workflows, so Ship Release's tag push alone would leave the release unpublished. The workflow now dispatches Release Binaries explicitly on the tag ref after pushing (`gh workflow run release.yml --ref <tag>`; `release.yml` resolves the tag via `GITHUB_REF_NAME`).
