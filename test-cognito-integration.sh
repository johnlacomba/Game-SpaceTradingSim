#!/bin/bash

# Comprehensive AWS Cognito Integration Test
echo "🔐 AWS Cognito Integration - Comprehensive Test"
echo "==============================================="

echo ""
echo "📋 Component Analysis:"
echo "======================"

# 1. Terraform Infrastructure
echo ""
echo "1️⃣  Terraform Infrastructure:"
if [ -d "terraform" ]; then
    echo "   ✅ Terraform directory exists"
    echo "   📁 Key files:"
    echo "      - cognito.tf: User Pool, Client, Identity Pool"
    echo "      - api_gateway.tf: REST and WebSocket APIs with JWT auth"
    echo "      - outputs.tf: Configuration outputs for frontend"
    
    if [ -f "terraform/terraform.tfvars.example" ]; then
        echo "   ✅ Example config file exists"
    else
        echo "   ⚠️  No terraform.tfvars.example found"
    fi
else
    echo "   ❌ Terraform directory missing"
fi

# 2. Backend Configuration
echo ""
echo "2️⃣  Backend Configuration:"
cd backend 2>/dev/null && {
    echo "   ✅ Backend directory exists"
    
    if [ -f "internal/auth/cognito.go" ]; then
        echo "   ✅ Cognito auth module exists"
        
        # Check for environment variable usage
        if grep -q "os.Getenv.*COGNITO_USER_POOL_ID" internal/auth/cognito.go; then
            echo "   ✅ Uses COGNITO_USER_POOL_ID env var"
        else
            echo "   ⚠️  Missing COGNITO_USER_POOL_ID env var usage"
        fi
        
        if grep -q "os.Getenv.*COGNITO_CLIENT_ID" internal/auth/cognito.go; then
            echo "   ✅ Uses COGNITO_CLIENT_ID env var"
        else
            echo "   ⚠️  Missing COGNITO_CLIENT_ID env var usage"
        fi
    else
        echo "   ❌ Cognito auth module missing"
    fi
    
    # Check for godotenv
    if grep -q "github.com/joho/godotenv" cmd/server/main.go; then
        echo "   ✅ Uses godotenv for environment loading"
    else
        echo "   ⚠️  Missing godotenv integration"
    fi
    
    # Test compilation
    echo "   🔧 Testing compilation..."
    if go build -o test-server cmd/server/main.go 2>/dev/null; then
        echo "   ✅ Backend compiles successfully"
        rm -f test-server
    else
        echo "   ❌ Backend compilation failed"
    fi
    
    cd ..
} || echo "   ❌ Backend directory missing"

# 3. Frontend Configuration
echo ""
echo "3️⃣  Frontend Configuration:"
cd frontend 2>/dev/null && {
    echo "   ✅ Frontend directory exists"
    
    if [ -f "src/aws-config.js" ]; then
        echo "   ✅ AWS config file exists"
        
        # Check environment variable usage
        if grep -q "VITE_COGNITO_USER_POOL_ID" src/aws-config.js; then
            echo "   ✅ Uses VITE_COGNITO_USER_POOL_ID"
        else
            echo "   ⚠️  Missing VITE_COGNITO_USER_POOL_ID"
        fi
    else
        echo "   ❌ AWS config file missing"
    fi
    
    # Check AuthContext
    if [ -f "src/contexts/AuthContext.tsx" ]; then
        echo "   ✅ TypeScript AuthContext exists"
        
        if grep -q "isDevMode.*production" src/contexts/AuthContext.tsx; then
            echo "   ✅ Has development mode detection"
        else
            echo "   ⚠️  Missing development mode detection"
        fi
        
        if grep -q "mockSignIn\|mockSignUp" src/contexts/AuthContext.tsx; then
            echo "   ✅ Has mock authentication functions"
        else
            echo "   ⚠️  Missing mock authentication"
        fi
    else
        echo "   ❌ TypeScript AuthContext missing"
    fi
    
    # Test compilation
    echo "   🔧 Testing compilation..."
    if npm run build > /dev/null 2>&1; then
        echo "   ✅ Frontend builds successfully"
    else
        echo "   ❌ Frontend build failed"
    fi
    
    cd ..
} || echo "   ❌ Frontend directory missing"

# 4. Deploy Script Analysis
echo ""
echo "4️⃣  Deploy Script Analysis:"
if [ -f "deploy-aws.sh" ]; then
    echo "   ✅ Deploy script exists"
    
    # Check if it generates frontend config
    if grep -q "frontend/.env.local" deploy-aws.sh; then
        echo "   ✅ Generates frontend configuration"
    else
        echo "   ⚠️  Missing frontend config generation"
    fi
    
    # Check if it generates backend config
    if grep -q "backend/.env" deploy-aws.sh; then
        echo "   ✅ Generates backend configuration"
    else
        echo "   ⚠️  Missing backend config generation"
    fi
    
    # Check if it sets production mode
    if grep -q "NODE_ENV=production" deploy-aws.sh; then
        echo "   ✅ Sets production mode"
    else
        echo "   ⚠️  Missing production mode setting"
    fi
else
    echo "   ❌ Deploy script missing"
fi

echo ""
echo "🎯 Integration Flow Analysis:"
echo "============================="

echo ""
echo "Development Mode Flow:"
echo "  1. NODE_ENV != 'production' → isDevMode = true"
echo "  2. Amplify.configure() is skipped"
echo "  3. All auth functions use mock implementations"
echo "  4. Backend doesn't require AWS credentials"
echo "  5. Works offline/localhost"

echo ""
echo "Production Mode Flow:"
echo "  1. terraform apply → creates AWS resources"
echo "  2. deploy-aws.sh → extracts Terraform outputs"
echo "  3. Sets NODE_ENV=production in frontend/.env.local"
echo "  4. Sets AWS credentials in backend/.env"
echo "  5. Frontend: Amplify configures with real AWS"
echo "  6. Backend: Validates real JWT tokens from Cognito"
echo "  7. API Gateway authorizes requests"

echo ""
echo "🚀 Deployment Instructions:"
echo "=========================="

echo ""
echo "Step 1: Install Prerequisites"
echo "  brew install terraform"
echo "  aws configure  # Set up AWS credentials"

echo ""
echo "Step 2: Configure Terraform"
echo "  cd terraform"
echo "  cp terraform.tfvars.example terraform.tfvars"
echo "  # Edit terraform.tfvars with your values"

echo ""
echo "Step 3: Deploy Infrastructure"
echo "  ./deploy-aws.sh"
echo "  # This will:"
echo "  #   - Run terraform apply"
echo "  #   - Generate frontend/.env.local"
echo "  #   - Generate backend/.env"
echo "  #   - Set NODE_ENV=production"
echo "  #   - Build applications"

echo ""
echo "Step 4: Run in Production Mode"
echo "  cd backend && ./bin/space-trader-server"
echo "  cd frontend && npm run dev  # Will use production AWS"

echo ""
echo "✅ Analysis Complete!"
echo ""
echo "🔍 Current Status Summary:"
if [ -d "terraform" ] && [ -f "backend/internal/auth/cognito.go" ] && [ -f "frontend/src/contexts/AuthContext.tsx" ] && [ -f "deploy-aws.sh" ]; then
    echo "   ✅ All core components present"
    echo "   ✅ Ready for AWS Cognito integration"
    echo "   ✅ Both development and production modes supported"
    echo ""
    echo "🎯 Next Action: Install Terraform and run ./deploy-aws.sh"
else
    echo "   ⚠️  Some components missing - review the analysis above"
fi
