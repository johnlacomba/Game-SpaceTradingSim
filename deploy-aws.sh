#!/bin/bash

# Deploy AWS infrastructure and Sphere of Influence
set -e

echo "🌌 Sphere of Influence AWS Deployment Script"
echo "================================================="

# Check if required tools are installed
check_requirements() {
    echo "📋 Checking requirements..."
    
    if ! command -v terraform &> /dev/null; then
        echo "❌ Terraform is not installed. Please install Terraform first."
        exit 1
    fi
    
    if ! command -v aws &> /dev/null; then
        echo "❌ AWS CLI is not installed. Please install AWS CLI first."
        exit 1
    fi
    
    if ! aws sts get-caller-identity &> /dev/null; then
        echo "❌ AWS CLI is not configured. Please run 'aws configure' first."
        exit 1
    fi
    
    echo "✅ All requirements satisfied"
}

# Deploy Terraform infrastructure
deploy_infrastructure() {
    echo "🏗️  Deploying AWS infrastructure..."
    
    cd terraform
    
    if [ ! -f "terraform.tfvars" ]; then
        echo "⚠️  terraform.tfvars not found. Creating from example..."
        cp terraform.tfvars.example terraform.tfvars
        echo "❗ Please edit terraform.tfvars with your specific values and run this script again."
        exit 1
    fi
    
    echo "   Initializing Terraform..."
    terraform init
    
    echo "   Planning deployment..."
    terraform plan -out=tfplan
    
    echo "   Applying infrastructure..."
    terraform apply tfplan
    
    echo "   Generating configuration..."
    terraform output -json > ../aws-outputs.json
    
    cd ..
    echo "✅ Infrastructure deployed successfully"
}

# Generate frontend configuration
generate_frontend_config() {
    echo "⚙️  Generating frontend configuration..."
    
    if [ ! -f "aws-outputs.json" ]; then
        echo "❌ aws-outputs.json not found. Please run infrastructure deployment first."
        exit 1
    fi
    
    # Extract values from Terraform outputs
    AWS_REGION=$(jq -r '.aws_config.value.region' aws-outputs.json)
    USER_POOL_ID=$(jq -r '.aws_config.value.userPoolId' aws-outputs.json)
    CLIENT_ID=$(jq -r '.aws_config.value.userPoolWebClientId' aws-outputs.json)
    IDENTITY_POOL_ID=$(jq -r '.aws_config.value.identityPoolId' aws-outputs.json)
    API_GATEWAY_URL=$(jq -r '.aws_config.value.apiGatewayUrl' aws-outputs.json)
    WEBSOCKET_URL=$(jq -r '.aws_config.value.websocketUrl' aws-outputs.json)
    COGNITO_DOMAIN=$(jq -r '.aws_config.value.cognitoDomain' aws-outputs.json)
    
    # Create frontend .env.local file
    cat > frontend/.env.local << EOF
# Generated AWS configuration
NODE_ENV=production
VITE_AWS_REGION=${AWS_REGION}
VITE_COGNITO_USER_POOL_ID=${USER_POOL_ID}
VITE_COGNITO_CLIENT_ID=${CLIENT_ID}
VITE_COGNITO_IDENTITY_POOL_ID=${IDENTITY_POOL_ID}
VITE_API_GATEWAY_URL=${API_GATEWAY_URL}
VITE_WEBSOCKET_URL=${WEBSOCKET_URL}
VITE_COGNITO_DOMAIN=${COGNITO_DOMAIN}
VITE_COGNITO_CALLBACK_URL=http://localhost:5173
VITE_COGNITO_LOGOUT_URL=http://localhost:5173
EOF
    
    echo "✅ Frontend configuration generated at frontend/.env.local"
    
    # Create backend .env file
    cat > backend/.env << EOF
# Backend AWS configuration
AWS_REGION=${AWS_REGION}
COGNITO_USER_POOL_ID=${USER_POOL_ID}
COGNITO_CLIENT_ID=${CLIENT_ID}
EOF
    
    echo "✅ Backend configuration generated at backend/.env"
}

# Install dependencies
install_dependencies() {
    echo "📦 Installing dependencies..."
    
    echo "   Installing frontend dependencies..."
    cd frontend
    npm install
    cd ..
    
    echo "   Installing backend dependencies..."
    cd backend
    go mod tidy
    cd ..
    
    echo "✅ Dependencies installed"
}

# Build applications
build_applications() {
    echo "🔨 Building applications..."
    
    echo "   Building frontend..."
    cd frontend
    npm run build
    cd ..
    
    echo "   Building backend..."
    cd backend
    go build -o bin/sphere-of-influence-server ./cmd/server
    cd ..
    
    echo "✅ Applications built successfully"
}

# Print deployment summary
print_summary() {
    echo ""
    echo "🎉 Deployment completed successfully!"
    echo "===================================="
    echo ""
    echo "📋 Next steps:"
    echo "1. Frontend development server: cd frontend && npm run dev"
    echo "2. Backend development server: cd backend && ./bin/sphere-of-influence-server"
    echo ""
    echo "🔗 Important URLs:"
    if [ -f "aws-outputs.json" ]; then
        echo "   API Gateway: $(jq -r '.api_gateway_url.value' aws-outputs.json)"
        echo "   WebSocket API: $(jq -r '.websocket_api_url.value' aws-outputs.json)"
        echo "   Cognito Domain: $(jq -r '.cognito_user_pool_domain.value' aws-outputs.json)"
    fi
    echo "   Local Frontend: http://localhost:5173"
    echo "   Local Backend: http://localhost:8080"
    echo ""
    echo "📖 For more information, see the README files in each directory."
}

# Parse command line arguments
SKIP_TERRAFORM=false
SKIP_BUILD=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-terraform)
            SKIP_TERRAFORM=true
            shift
            ;;
        --skip-build)
            SKIP_BUILD=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --skip-terraform    Skip Terraform infrastructure deployment"
            echo "  --skip-build        Skip building applications"
            echo "  -h, --help          Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use -h or --help for usage information"
            exit 1
            ;;
    esac
done

# Main execution
main() {
    check_requirements
    
    if [ "$SKIP_TERRAFORM" = false ]; then
        deploy_infrastructure
        generate_frontend_config
    else
        echo "⏭️  Skipping Terraform deployment"
    fi
    
    install_dependencies
    
    if [ "$SKIP_BUILD" = false ]; then
        build_applications
    else
        echo "⏭️  Skipping build step"
    fi
    
    print_summary
}

# Run main function
main
