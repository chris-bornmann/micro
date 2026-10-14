# mapd as an ECS service on Fargate.
#
# Games live in mapd's memory, so the service runs exactly one task: a
# second would not know the first one's games. For the same reason a deploy
# stops the old task before starting the new one (minimum healthy percent
# 0), rather than briefly running both. Players see a short outage, and
# every game in progress is lost.

locals {
  cognito_domain = "${aws_cognito_user_pool_domain.game.domain}.auth.${var.region}.amazoncognito.com"
}

resource "aws_ecs_cluster" "game" {
  name = var.name
}

resource "aws_cloudwatch_log_group" "mapd" {
  name              = "/ecs/${var.name}/mapd"
  retention_in_days = 30
}

# The role ECS itself uses to start the task: pull the image and write
# logs. mapd calls no AWS API, so the task has no role of its own.
resource "aws_iam_role" "execution" {
  name = "${var.name}-task-execution"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "execution" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_ecs_task_definition" "mapd" {
  family                   = "${var.name}-mapd"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  # The smallest size Fargate offers. A turn-based game for a handful of
  # players needs no more.
  cpu                = 256
  memory             = 512
  execution_role_arn = aws_iam_role.execution.arn

  # push.sh builds for arm64, which costs less on Fargate than x86_64.
  runtime_platform {
    operating_system_family = "LINUX"
    cpu_architecture        = "ARM64"
  }

  container_definitions = jsonencode([{
    name      = "mapd"
    image     = "${aws_ecr_repository.mapd.repository_url}:${var.image_tag}"
    essential = true
    portMappings = [{
      containerPort = 8080
      protocol      = "tcp"
    }]
    command = concat([
      "-addr", ":8080",
      "-web-dir", "/web",
      "-auth", "cognito",
      "-cognito-pool", aws_cognito_user_pool.players.id,
      "-cognito-client", aws_cognito_user_pool_client.game.id,
      "-cognito-domain", local.cognito_domain,
    ], var.mapd_flags)
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.mapd.name
        awslogs-region        = var.region
        awslogs-stream-prefix = "mapd"
      }
    }
  }])
}

resource "aws_ecs_service" "mapd" {
  name            = "mapd"
  cluster         = aws_ecs_cluster.game.id
  task_definition = aws_ecs_task_definition.mapd.arn
  launch_type     = "FARGATE"
  desired_count   = 1

  # Stop, then start: never two tasks at once (see the top of this file).
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 100

  network_configuration {
    subnets          = data.aws_subnets.default.ids
    security_groups  = [aws_security_group.task.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.mapd.arn
    container_name   = "mapd"
    container_port   = 8080
  }

  depends_on = [aws_lb_listener.http]
}
