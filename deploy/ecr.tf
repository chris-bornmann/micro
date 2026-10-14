# The repository push.sh pushes mapd images to.
resource "aws_ecr_repository" "mapd" {
  name = "${var.name}/mapd"

  image_scanning_configuration {
    scan_on_push = true
  }
}

# Keep the last few images, for rolling back, and let older ones go.
resource "aws_ecr_lifecycle_policy" "mapd" {
  repository = aws_ecr_repository.mapd.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "Keep the 10 most recent images"
      selection = {
        tagStatus   = "any"
        countType   = "imageCountMoreThan"
        countNumber = 10
      }
      action = { type = "expire" }
    }]
  })
}
