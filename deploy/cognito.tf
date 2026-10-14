# The Cognito user pool players sign in with.
#
# Sign-up is by invitation only: an administrator creates each player (see
# README.md), and Cognito emails them a temporary password to change the
# first time they sign in. Nothing about a player is stored anywhere else;
# mapd knows them only by the pool's ID for them.

locals {
  # Where the hosted sign-in page may send a player back to: the game
  # itself, a local mapd or web dev server, and the terminal and desktop
  # clients, which listen on a fixed localhost port while signing in.
  # Cognito allows plain http only for localhost.
  callback_urls = [
    "https://${aws_cloudfront_distribution.game.domain_name}/",
    "http://localhost:8080/",
    "http://localhost:5173/",
    "http://localhost:47615/callback",
  ]
}

resource "aws_cognito_user_pool" "players" {
  name = "${var.name}-players"

  # Players sign in with their email address, which Cognito verifies.
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  admin_create_user_config {
    allow_admin_create_user_only = true
  }

  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }

  password_policy {
    minimum_length                   = 12
    require_lowercase                = false
    require_uppercase                = false
    require_numbers                  = false
    require_symbols                  = false
    temporary_password_validity_days = 7
  }

  # Keep players from being lost to a stray terraform destroy.
  deletion_protection = "ACTIVE"
}

# The hosted sign-in page, at <prefix>.auth.<region>.amazoncognito.com.
resource "aws_cognito_user_pool_domain" "game" {
  domain       = var.cognito_domain_prefix
  user_pool_id = aws_cognito_user_pool.players.id
}

# The app client every game client signs in through. It is public (no
# secret): the clients run on players' own devices, where a secret could not
# be kept, so they prove themselves with PKCE instead.
resource "aws_cognito_user_pool_client" "game" {
  name            = "${var.name}-game"
  user_pool_id    = aws_cognito_user_pool.players.id
  generate_secret = false

  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "email"]
  supported_identity_providers         = ["COGNITO"]
  callback_urls                        = local.callback_urls
  logout_urls                          = local.callback_urls

  explicit_auth_flows = ["ALLOW_REFRESH_TOKEN_AUTH"]

  # An hour per access token, refreshed quietly by the clients; signing in
  # again every 30 days.
  access_token_validity  = 1
  id_token_validity      = 1
  refresh_token_validity = 30
  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }

  prevent_user_existence_errors = "ENABLED"
  enable_token_revocation       = true
}
