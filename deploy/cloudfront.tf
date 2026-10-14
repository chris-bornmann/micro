# CloudFront puts the game on HTTPS at a *.cloudfront.net address, with
# AWS's own certificate, so no domain name is needed. Cognito insists on
# HTTPS for any sign-in redirect that isn't to localhost.
#
# Nothing is cached: every request, the web client's files included, goes
# to mapd. The files are small, and this way a deploy is seen at once.

data "aws_cloudfront_cache_policy" "disabled" {
  name = "Managed-CachingDisabled"
}

# Pass everything through (headers including Authorization, the query
# string, cookies), except Host, which must be the load balancer's own.
data "aws_cloudfront_origin_request_policy" "all_but_host" {
  name = "Managed-AllViewerExceptHostHeader"
}

resource "aws_cloudfront_distribution" "game" {
  enabled         = true
  comment         = "micro: HTTPS in front of the load balancer"
  is_ipv6_enabled = true
  # North America and Europe only, the cheapest class.
  price_class = "PriceClass_100"

  origin {
    origin_id   = "alb"
    domain_name = aws_lb.game.dns_name

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "http-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    target_origin_id         = "alb"
    viewer_protocol_policy   = "redirect-to-https"
    allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods           = ["GET", "HEAD"]
    cache_policy_id          = data.aws_cloudfront_cache_policy.disabled.id
    origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_but_host.id
    compress                 = true
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}
