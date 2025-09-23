# ECS Cluster (optional, for containerized deployment)
resource "aws_ecs_cluster" "main" {
  count = var.enable_ecs ? 1 : 0
  name  = "${var.project_name}-${var.environment}"

  setting {
    name  = "containerInsights"
    value = "enabled"
  }

  tags = local.common_tags
}

# ECS Task Definition for Backend
resource "aws_ecs_task_definition" "backend" {
  count                    = var.enable_ecs ? 1 : 0
  family                   = "${var.project_name}-${var.environment}-backend"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = aws_iam_role.ecs_execution[0].arn
  task_role_arn           = aws_iam_role.ecs_task[0].arn

  container_definitions = jsonencode([
    {
      name  = "backend"
      image = "${aws_ecr_repository.backend[0].repository_url}:latest"
      
      portMappings = [
        {
          containerPort = 8080
          protocol      = "tcp"
        }
      ]

      environment = [
        {
          name  = "PORT"
          value = "8080"
        },
        {
          name  = "AWS_REGION"
          value = var.aws_region
        },
        {
          name  = "COGNITO_USER_POOL_ID"
          value = aws_cognito_user_pool.main.id
        },
        {
          name  = "COGNITO_CLIENT_ID"
          value = aws_cognito_user_pool_client.main.id
        }
      ]

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.backend[0].name
          "awslogs-region"        = var.aws_region
          "awslogs-stream-prefix" = "ecs"
        }
      }

      essential = true
    }
  ])

  tags = local.common_tags
}

# ECR Repository for Backend
resource "aws_ecr_repository" "backend" {
  count = var.enable_ecs ? 1 : 0
  name  = "${var.project_name}-${var.environment}-backend"

  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = local.common_tags
}

# CloudWatch Log Group for Backend
resource "aws_cloudwatch_log_group" "backend" {
  count             = var.enable_ecs ? 1 : 0
  name              = "/ecs/${var.project_name}-${var.environment}-backend"
  retention_in_days = 14

  tags = local.common_tags
}

# ECS Execution Role
resource "aws_iam_role" "ecs_execution" {
  count = var.enable_ecs ? 1 : 0
  name  = "${var.project_name}-${var.environment}-ecs-execution"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
      }
    ]
  })

  tags = local.common_tags
}

resource "aws_iam_role_policy_attachment" "ecs_execution" {
  count      = var.enable_ecs ? 1 : 0
  role       = aws_iam_role.ecs_execution[0].name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# ECS Task Role
resource "aws_iam_role" "ecs_task" {
  count = var.enable_ecs ? 1 : 0
  name  = "${var.project_name}-${var.environment}-ecs-task"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
      }
    ]
  })

  tags = local.common_tags
}

resource "aws_iam_role_policy" "ecs_task" {
  count = var.enable_ecs ? 1 : 0
  name  = "${var.project_name}-${var.environment}-ecs-task-policy"
  role  = aws_iam_role.ecs_task[0].id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "cognito-idp:GetUser",
          "cognito-idp:AdminGetUser",
          "execute-api:ManageConnections"
        ]
        Resource = [
          aws_cognito_user_pool.main.arn,
          "${aws_apigatewayv2_api.websocket.execution_arn}/*"
        ]
      }
    ]
  })
}
