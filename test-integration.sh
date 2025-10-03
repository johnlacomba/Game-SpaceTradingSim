#!/bin/bash

# Quick test script to verify AWS Cognito integration
set -e

echo "🧪 Testing AWS Cognito Integration"
echo "=================================="

# Test backend compilation
echo "📦 Testing backend compilation..."
cd backend
if go build -o bin/sphere-of-influence-server ./cmd/server; then
    echo "✅ Backend compiles successfully"
else
    echo "❌ Backend compilation failed"
    exit 1
fi
cd ..

# Test frontend compilation  
echo "📦 Testing frontend compilation..."
cd frontend
if npm run build > /dev/null 2>&1; then
    echo "✅ Frontend builds successfully"
else
    echo "❌ Frontend build failed"
    exit 1
fi
cd ..

# Check required files exist
echo "📋 Checking required files..."

REQUIRED_FILES=(
    "terraform/main.tf"
    "terraform/cognito.tf"
    "terraform/api_gateway.tf"
    "terraform/variables.tf"
    "terraform/outputs.tf"
    "backend/internal/auth/cognito.go"
    "frontend/src/contexts/AuthContext.jsx"
    "frontend/src/components/LoginForm.jsx"
    "frontend/src/aws-config.js"
    "deploy-aws.sh"
)

for file in "${REQUIRED_FILES[@]}"; do
    if [ -f "$file" ]; then
        echo "✅ $file"
    else
        echo "❌ $file (missing)"
    fi
done

# Check if AWS CLI is available (optional)
if command -v aws &> /dev/null; then
    echo "✅ AWS CLI is available"
    if aws sts get-caller-identity &> /dev/null; then
        echo "✅ AWS CLI is configured"
    else
        echo "⚠️  AWS CLI is not configured (run 'aws configure')"
    fi
else
    echo "⚠️  AWS CLI not found (install for deployment)"
fi

# Check if Terraform is available (optional)
if command -v terraform &> /dev/null; then
    echo "✅ Terraform is available"
else
    echo "⚠️  Terraform not found (install for AWS deployment)"
fi

echo ""
echo "🎯 Next Steps:"
echo "1. Configure AWS CLI: aws configure"
echo "2. Edit terraform/terraform.tfvars with your settings"
echo "3. Deploy AWS infrastructure: ./deploy-aws.sh"
echo "4. Or run locally for development:"
echo "   - Backend: cd backend && go run cmd/server/main.go"
echo "   - Frontend: cd frontend && npm run dev"
echo ""
echo "📖 See README.md for detailed instructions"
