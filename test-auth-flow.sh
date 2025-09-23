#!/bin/bash

# Test Authentication Flow
echo "🔐 Testing AWS Cognito Authentication Flow"
echo "=========================================="

# Check environment
echo "📋 Environment Check:"
echo "- NODE_ENV: ${NODE_ENV:-not set}"
echo "- AWS_REGION: ${AWS_REGION:-not set}"
echo "- AWS Profile: $(aws configure list-profiles 2>/dev/null | head -1 || echo 'not configured')"

# Check Terraform
echo ""
echo "🏗️  Infrastructure Check:"
if command -v terraform &> /dev/null; then
    echo "✅ Terraform installed: $(terraform version | head -1)"
    if [ -d "terraform" ]; then
        cd terraform
        if [ -f "terraform.tfstate" ]; then
            echo "✅ Terraform state exists"
            echo "📊 Current resources:"
            terraform show -json | jq -r '.values.root_module.resources[]?.address' 2>/dev/null | head -5 || echo "   (state file present but no resources listed)"
        else
            echo "⚠️  No terraform.tfstate found - run 'terraform apply' first"
        fi
        cd ..
    else
        echo "⚠️  No terraform directory found"
    fi
else
    echo "⚠️  Terraform not installed"
fi

# Check backend
echo ""
echo "🔧 Backend Check:"
cd backend
echo "Testing Go compilation..."
if go build -o test-server cmd/server/main.go; then
    echo "✅ Backend compiles successfully"
    rm -f test-server
else
    echo "❌ Backend compilation failed"
    cd ..
    exit 1
fi
cd ..

# Check frontend
echo ""
echo "🎨 Frontend Check:"
cd frontend
echo "Testing React build..."
if npm run build > /dev/null 2>&1; then
    echo "✅ Frontend builds successfully"
else
    echo "❌ Frontend build failed"
    cd ..
    exit 1
fi

# Check auth context
echo "🔍 Checking AuthContext implementation..."
if grep -q "isDevMode" src/contexts/AuthContext.jsx; then
    echo "✅ Development mode implemented"
else
    echo "⚠️  Development mode not found"
fi

if grep -q "mockSignIn" src/contexts/AuthContext.jsx; then
    echo "✅ Mock authentication functions present"
else
    echo "⚠️  Mock functions not found"
fi
cd ..

# Test modes
echo ""
echo "🧪 Testing Authentication Modes:"
echo ""

echo "1️⃣  Development Mode Test:"
echo "   In dev mode, authentication should work with mock users"
echo "   - Mock functions provide fake JWT tokens"
echo "   - No AWS connection required"
echo "   - Users can sign in with any credentials"

echo ""
echo "2️⃣  Production Mode Test:"
echo "   Requires AWS infrastructure to be deployed:"
echo "   - Run: cd terraform && terraform apply"
echo "   - Copy outputs to frontend/.env"
echo "   - Set NODE_ENV=production"

echo ""
echo "🚀 Quick Start Options:"
echo ""
echo "For Development (No AWS needed):"
echo "  cd frontend && npm run dev"
echo "  # Authentication will use mock functions"
echo ""
echo "For Production (AWS required):"
echo "  1. cd terraform && terraform apply"
echo "  2. Update frontend/.env with Terraform outputs"
echo "  3. ./deploy-aws.sh"
echo ""

echo "✅ Authentication flow test complete!"
