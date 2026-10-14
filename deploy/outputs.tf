output "game_url" {
  description = "Where to play."
  value       = "https://${aws_cloudfront_distribution.game.domain_name}/"
}

output "ecr_repository_url" {
  description = "Where push.sh pushes images."
  value       = aws_ecr_repository.mapd.repository_url
}

output "user_pool_id" {
  description = "The Cognito user pool, for inviting players (see README.md)."
  value       = aws_cognito_user_pool.players.id
}

output "sign_in_domain" {
  description = "The hosted sign-in page's domain."
  value       = local.cognito_domain
}

output "app_client_id" {
  description = "The app client the game clients sign in through."
  value       = aws_cognito_user_pool_client.game.id
}
