variable "region" {
  description = "AWS region to run everything in."
  type        = string
  default     = "us-east-1"
}

variable "name" {
  description = "Prefix for every resource's name."
  type        = string
  default     = "micro"
}

variable "image_tag" {
  description = "Tag of the mapd image in the ECR repository to run, as push.sh printed it."
  type        = string
}

variable "cognito_domain_prefix" {
  description = "Prefix of the Cognito sign-in page's domain, <prefix>.auth.<region>.amazoncognito.com. It must be unique across all of AWS."
  type        = string
}

variable "mapd_flags" {
  description = "Extra mapd flags, such as [\"-seed\", \"4\"]."
  type        = list(string)
  default     = []
}
