variable "aws_region" {
  description = "AWS region for resources"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Name of the project"
  type        = string
  default     = "space-trading-sim"
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "dev"
}

variable "domain_name" {
  description = "Domain name for the application (optional for HTTPS)"
  type        = string
  default     = ""
}

variable "cognito_callback_urls" {
  description = "Callback URLs for Cognito"
  type        = list(string)
  default     = ["http://localhost:5173", "https://localhost:5173"]
}

variable "cognito_logout_urls" {
  description = "Logout URLs for Cognito"
  type        = list(string)
  default     = ["http://localhost:5173", "https://localhost:5173"]
}

variable "enable_ecs" {
  description = "Whether to enable ECS deployment"
  type        = bool
  default     = false
}
