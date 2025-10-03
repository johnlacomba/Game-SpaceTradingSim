# API Gateway REST API
resource "aws_api_gateway_rest_api" "main" {
  name        = "${var.project_name}-${var.environment}-api"
  description = "Sphere of Influence Game API"

  endpoint_configuration {
    types = ["REGIONAL"]
  }

  tags = local.common_tags
}

# API Gateway Cognito Authorizer
resource "aws_api_gateway_authorizer" "cognito" {
  name                   = "cognito-authorizer"
  rest_api_id           = aws_api_gateway_rest_api.main.id
  type                  = "COGNITO_USER_POOLS"
  identity_source       = "method.request.header.Authorization"
  provider_arns         = [aws_cognito_user_pool.main.arn]
  authorizer_credentials = aws_iam_role.api_gateway_cognito.arn
}

# IAM role for API Gateway to access Cognito
resource "aws_iam_role" "api_gateway_cognito" {
  name = "${var.project_name}-${var.environment}-api-gateway-cognito"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "apigateway.amazonaws.com"
        }
      }
    ]
  })

  tags = local.common_tags
}

resource "aws_iam_role_policy" "api_gateway_cognito" {
  name = "${var.project_name}-${var.environment}-api-gateway-cognito-policy"
  role = aws_iam_role.api_gateway_cognito.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "cognito-idp:GetUser"
        ]
        Resource = aws_cognito_user_pool.main.arn
      }
    ]
  })
}

# CORS Options method for preflight requests
resource "aws_api_gateway_method" "options" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_rest_api.main.root_resource_id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_rest_api.main.root_resource_id
  http_method = aws_api_gateway_method.options.http_method
  type        = "MOCK"

  request_templates = {
    "application/json" = jsonencode({
      statusCode = 200
    })
  }
}

resource "aws_api_gateway_method_response" "options" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_rest_api.main.root_resource_id
  http_method = aws_api_gateway_method.options.http_method
  status_code = "200"

  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_rest_api.main.root_resource_id
  http_method = aws_api_gateway_method.options.http_method
  status_code = aws_api_gateway_method_response.options.status_code

  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = "'Content-Type,X-Amz-Date,Authorization,X-Api-Key,X-Amz-Security-Token'"
    "method.response.header.Access-Control-Allow-Methods" = "'GET,POST,PUT,DELETE,OPTIONS'"
    "method.response.header.Access-Control-Allow-Origin"  = "'*'"
  }
}

# WebSocket API for real-time game communication
resource "aws_apigatewayv2_api" "websocket" {
  name                       = "${var.project_name}-${var.environment}-websocket"
  protocol_type             = "WEBSOCKET"
  route_selection_expression = "$request.body.action"

  tags = local.common_tags
}

# WebSocket Routes
resource "aws_apigatewayv2_route" "websocket_connect" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "$connect"
  target    = "integrations/${aws_apigatewayv2_integration.websocket_connect.id}"
  
  # Optional: Add authorization later if needed
  # authorization_type = "AWS_IAM"
}

resource "aws_apigatewayv2_route" "websocket_disconnect" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "$disconnect"
  target    = "integrations/${aws_apigatewayv2_integration.websocket_disconnect.id}"
}

resource "aws_apigatewayv2_route" "websocket_default" {
  api_id    = aws_apigatewayv2_api.websocket.id
  route_key = "$default"
  target    = "integrations/${aws_apigatewayv2_integration.websocket_default.id}"
}

# WebSocket Route Responses
resource "aws_apigatewayv2_route_response" "websocket_connect" {
  api_id             = aws_apigatewayv2_api.websocket.id
  route_id           = aws_apigatewayv2_route.websocket_connect.id
  route_response_key = "$default"
}

resource "aws_apigatewayv2_route_response" "websocket_disconnect" {
  api_id             = aws_apigatewayv2_api.websocket.id
  route_id           = aws_apigatewayv2_route.websocket_disconnect.id
  route_response_key = "$default"
}

resource "aws_apigatewayv2_route_response" "websocket_default" {
  api_id             = aws_apigatewayv2_api.websocket.id
  route_id           = aws_apigatewayv2_route.websocket_default.id
  route_response_key = "$default"
}

# WebSocket Integrations (Mock integrations for now)
resource "aws_apigatewayv2_integration" "websocket_connect" {
  api_id           = aws_apigatewayv2_api.websocket.id
  integration_type = "MOCK"
}

resource "aws_apigatewayv2_integration" "websocket_disconnect" {
  api_id           = aws_apigatewayv2_api.websocket.id
  integration_type = "MOCK"
}

resource "aws_apigatewayv2_integration" "websocket_default" {
  api_id           = aws_apigatewayv2_api.websocket.id
  integration_type = "MOCK"
}

# API Gateway Deployment
resource "aws_api_gateway_deployment" "main" {
  depends_on = [
    aws_api_gateway_method.options,
    aws_api_gateway_integration.options,
  ]

  rest_api_id = aws_api_gateway_rest_api.main.id

  lifecycle {
    create_before_destroy = true
  }
}

# API Gateway Stage
resource "aws_api_gateway_stage" "main" {
  deployment_id = aws_api_gateway_deployment.main.id
  rest_api_id   = aws_api_gateway_rest_api.main.id
  stage_name    = var.environment

  tags = local.common_tags
}

# WebSocket Deployment  
resource "aws_apigatewayv2_deployment" "websocket" {
  api_id      = aws_apigatewayv2_api.websocket.id
  description = "WebSocket deployment for ${var.environment}"

  depends_on = [
    aws_apigatewayv2_route.websocket_connect,
    aws_apigatewayv2_route.websocket_disconnect,
    aws_apigatewayv2_route.websocket_default,
    aws_apigatewayv2_integration.websocket_connect,
    aws_apigatewayv2_integration.websocket_disconnect,
    aws_apigatewayv2_integration.websocket_default,
    aws_apigatewayv2_route_response.websocket_connect,
    aws_apigatewayv2_route_response.websocket_disconnect,
    aws_apigatewayv2_route_response.websocket_default,
  ]

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_apigatewayv2_stage" "websocket" {
  api_id        = aws_apigatewayv2_api.websocket.id
  deployment_id = aws_apigatewayv2_deployment.websocket.id
  name          = var.environment

  tags = local.common_tags
}
