# deploy

Terraform that runs micro on AWS:

```
player ──HTTPS──▶ CloudFront ──HTTP──▶ load balancer ──▶ mapd on Fargate (one task)
   │
   └── signs in on Cognito's hosted page
```

| File | What it sets up |
|---|---|
| `versions.tf` | The AWS provider, and Terraform state in S3 |
| `network.tf` | The default VPC's public subnets; security groups that let only CloudFront reach the load balancer, and only the load balancer reach mapd |
| `ecr.tf` | The image repository `push.sh` pushes to |
| `ecs.tf` | The Fargate service: one mapd task, with sign-in required |
| `alb.tf` | The load balancer, health-checking `/healthz` |
| `cloudfront.tf` | HTTPS at a `*.cloudfront.net` address, so no domain name is needed |
| `cognito.tf` | The user pool players sign in with: invitation only |
| `outputs.tf` | The game's address, and the IDs the steps below need |

Games live in mapd's memory. A deploy, or anything else that restarts the
task, ends every game in progress and causes a short outage.

## Before the first deploy

You need the AWS CLI signed in to your account, Terraform 1.10 or later, and
Docker.

1. Create an S3 bucket for Terraform's state, once, by hand. Bucket names are
   global, so pick your own:

   ```sh
   aws s3 mb s3://<your-state-bucket> --region us-east-1
   ```

2. Pick a prefix for the sign-in page's domain, also unique across AWS, and
   put it in `deploy/terraform.tfvars`:

   ```hcl
   cognito_domain_prefix = "<your-prefix>"
   ```

3. Initialize, then create just the image repository, so there is somewhere
   to push to:

   ```sh
   terraform -chdir=deploy init -backend-config="bucket=<your-state-bucket>" -backend-config="region=us-east-1"
   terraform -chdir=deploy apply -target=aws_ecr_repository.mapd -var image_tag=none
   ```

   Commit the `.terraform.lock.hcl` that `init` writes.

## Deploying

Build and push an image, then point the service at it. `push.sh` prints the
tag, which is the current git commit.

```sh
deploy/push.sh
terraform -chdir=deploy apply -var image_tag=<tag>
```

The first full apply takes several minutes, most of it CloudFront. It ends by
printing `game_url`, where the game is.

## Inviting a player

Sign-up is by invitation only. Create the player, and Cognito emails them a
temporary password, which they change the first time they sign in:

```sh
aws cognito-idp admin-create-user \
  --user-pool-id "$(terraform -chdir=deploy output -raw user_pool_id)" \
  --username <their-email> \
  --user-attributes Name=email,Value=<their-email> Name=email_verified,Value=true
```

## Cost

Roughly, from AWS's published prices: the Fargate task (0.25 vCPU, 512 MB,
arm64) is under $10 a month, the load balancer about $16, and CloudFront,
ECR, CloudWatch and Cognito cost nothing or nearly nothing at this scale.

## Checking changes

```sh
terraform -chdir=deploy fmt -check
terraform -chdir=deploy validate
```
