# Cognito User Pool Outputs
output "cognito_user_pool_id" {
  description = "ID of the Cognito User Pool"
  value       = aws_cognito_user_pool.main.id
}

output "cognito_user_pool_arn" {
  description = "ARN of the Cognito User Pool"
  value       = aws_cognito_user_pool.main.arn
}

output "cognito_user_pool_client_id" {
  description = "ID of the Cognito User Pool Client"
  value       = aws_cognito_user_pool_client.main.id
}

output "cognito_user_pool_domain" {
  description = "Domain of the Cognito User Pool"
  value       = aws_cognito_user_pool_domain.main.domain
}

output "cognito_identity_pool_id" {
  description = "ID of the Cognito Identity Pool"
  value       = aws_cognito_identity_pool.main.id
}

# API Gateway Outputs
output "api_gateway_url" {
  description = "URL of the API Gateway REST API"
  value       = "https://${aws_api_gateway_rest_api.main.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
}

output "api_gateway_id" {
  description = "ID of the API Gateway REST API"
  value       = aws_api_gateway_rest_api.main.id
}

output "websocket_api_url" {
  description = "URL of the WebSocket API"
  value       = "wss://${aws_apigatewayv2_api.websocket.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
}

output "websocket_api_id" {
  description = "ID of the WebSocket API"
  value       = aws_apigatewayv2_api.websocket.id
}

# ECS Outputs (conditional)
output "ecr_repository_url" {
  description = "URL of the ECR repository for backend"
  value       = var.enable_ecs ? aws_ecr_repository.backend[0].repository_url : null
}

output "ecs_cluster_name" {
  description = "Name of the ECS cluster"
  value       = var.enable_ecs ? aws_ecs_cluster.main[0].name : null
}

# AWS Configuration for Frontend
output "aws_config" {
  description = "AWS configuration for frontend application"
  value = {
    region                = var.aws_region
    userPoolId           = aws_cognito_user_pool.main.id
    userPoolWebClientId  = aws_cognito_user_pool_client.main.id
    identityPoolId       = aws_cognito_identity_pool.main.id
    apiGatewayUrl        = "https://${aws_api_gateway_rest_api.main.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
    websocketUrl         = "wss://${aws_apigatewayv2_api.websocket.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
    cognitoDomain        = "https://${aws_cognito_user_pool_domain.main.domain}.auth.${var.aws_region}.amazoncognito.com"
  }
}

# Hosted UI URLs
output "cognito_hosted_ui_base_url" {
  description = "Base URL for the Cognito Hosted UI"
  value       = "https://${aws_cognito_user_pool_domain.main.domain}.auth.${var.aws_region}.amazoncognito.com"
}

output "cognito_google_redirect_uri" {
  description = "Redirect URI to add in Google Cloud OAuth Client (Authorized redirect URIs)"
  value       = "https://${aws_cognito_user_pool_domain.main.domain}.auth.${var.aws_region}.amazoncognito.com/oauth2/idpresponse"
}
