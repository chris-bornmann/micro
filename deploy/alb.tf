# The load balancer in front of the task. It speaks plain HTTP, and only to
# CloudFront (see network.tf), which gives players HTTPS on its own domain.
# Behind a load balancer, the task can be replaced without the address
# CloudFront uses changing.

resource "aws_lb" "game" {
  name               = var.name
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets            = data.aws_subnets.default.ids
}

resource "aws_lb_target_group" "mapd" {
  name        = "${var.name}-mapd"
  port        = 8080
  protocol    = "HTTP"
  target_type = "ip"
  vpc_id      = data.aws_vpc.default.id

  # The one route that needs no sign-in.
  health_check {
    path    = "/healthz"
    matcher = "200"
  }

  # The old task is already stopped by the time a deploy drains it, so
  # there is nothing to wait for.
  deregistration_delay = 5
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.game.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.mapd.arn
  }
}
