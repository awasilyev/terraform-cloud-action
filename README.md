# terraform-cloud-action

This GitHub Action allows triggering a new plan or run in Terraform Cloud. It can be used alongside events such as crons to regularly trigger a Terraform Cloud run which might be helpful in scenarios such as drift detection. For deployment setups where a Production environment configuration is stored separately from the development repository this might also be used to trigger a run upon a deployment request.

## Inputs

### `tfe-token`

**Required** The API token granting access to communicate with Terraform Cloud.

### `organization`

**Required** The organization name containing the workspace to trigger.

### `workspace`

**Required** The workspace name to trigger.

### `json-vars`

**Optional** JSON-encoded list of variables to update the workspace before triggering the run. Default `"[]"`.

This property allows arbitrary updating of variables before the run starts. It can be used to, say, update a value representing the git SHA or Docker image tag that was pushed as part of this operation.

The property expects an array of objects formatted in a way that the TFE API expects. For example, a simple update of two keys' values might look like:

```yml
with:
  json-vars: "[{'key': 'foo', 'value': 'bar'}, {'key': 'baz', 'value': 'guz'}]"
```

Additional properties such as `sensitive` and `hcl` are also available. See the documentation on [VariableUpdateOptions](https://pkg.go.dev/github.com/hashicorp/go-tfe#VariableUpdateOptions) for details.

### `message`

**Optional** The message to be associated with this run. Default `"Triggered via terraform-cloud-action GitHub Action"`.

### `url`

**Optional** The location of the Terraform Cloud installation. Default `"https://app.terraform.io"`.

### `wait`

**Optional** If true, will block until the run is marked as completed. Default `"true"`.

**WARNING:** Waiting on runs that require external user input can expend GitHub Actions minutes. Consider your GitHub Actions budget and Workspace configuration before using this setting.

Regardless of the `wait` setting this Action defines a 60 minute timeout on its wait time as a precaution for endless runs.

## Outputs

### `run-id`

The ID of the created run.

### `run-url`

The URL to view the run.

## Docker Image

This action now uses a pre-built Docker image from GitHub Container Registry (ghcr.io) instead of building from source. The image is automatically built and pushed on:

- **Push to main/master branch**: Creates `ghcr.io/awasilyev/terraform-cloud-action:main` tag
- **Pull requests**: Creates branch-specific tags for testing
- **Releases**: Creates versioned tags (e.g., `ghcr.io/awasilyev/terraform-cloud-action:v1.0.0`)

### Build Workflows

The repository includes two GitHub Actions workflows:

1. **`.github/workflows/build-and-push.yml`**: Builds and pushes images on every push and pull request
2. **`.github/workflows/release.yml`**: Builds and pushes versioned images when releases are published

These workflows use GitHub's built-in container registry authentication and caching for efficient builds.

## Example usage

```yml
uses: awasilyev/terraform-cloud-action@v1
with:
  tfe-token: ${{ secrets.TFE_TOKEN }}
  organization: "your-org"
  workspace: "your-workspace"
```
