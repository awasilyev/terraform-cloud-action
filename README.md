# terraform-cloud-action

This GitHub Action allows triggering a new plan or run in Terraform Cloud. It can be used alongside events such as crons to regularly trigger a Terraform Cloud run which might be helpful in scenarios such as drift detection. For deployment setups where a Production environment configuration is stored separately from the development repository this might also be used to trigger a run upon a deployment request.

## Inputs

### `tfe-token`

**Required** The API token granting access to communicate with Terraform Cloud.

### `organization`

**Required** The organization name containing the workspace to trigger.

### `workspace`

**Required** The workspace name to trigger.

### `message`

**Optional** The message to be associated with this run. Default `"Triggered via terraform-cloud-action GitHub Action"`.

### `url`

**Optional** The location of the Terraform Cloud installation. Default `"https://app.terraform.io"`.

## Outputs

### `run-url`

The URL to view the completed run.

## Example usage

```yml
uses: taiidani/terraform-cloud-action@v1
with:
  tfe-token: ${{ secrets.TFE_TOKEN }}
  organization: "taiidani"
  workspace: "tfc-workspace"
```
