# Terraform for running micro on AWS. See README.md for how to use it.

terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  # State lives in an S3 bucket you create once by hand. Its name is given
  # at init time (terraform init -backend-config=...), so it is not
  # written into the repository.
  backend "s3" {
    key          = "micro/terraform.tfstate"
    use_lockfile = true
  }
}

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project = var.name
    }
  }
}
