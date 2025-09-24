# Terraform Configuration for Space Trading Simulation

This directory contains Terraform configurations to deploy AWS infrastructure for the Space Trading Simulation game with AWS Cognito authentication and API Gateway.

## Prerequisites

1. **AWS CLI** - Install and configure with appropriate credentials
2. **Terraform** - Version 1.0 or later
3. **AWS Account** - With permissions to create the required resources

## AWS Resources Created

- **AWS Cognito User Pool** - For user authentication
- **AWS Cognito Identity Pool** - For AWS resource access
- **API Gateway REST API** - For HTTP endpoints
- **API Gateway WebSocket API** - For real-time game communication
- **IAM Roles and Policies** - For proper permissions
- **ECR Repository** (optional) - For container images
- **ECS Cluster** (optional) - For containerized deployment
- **CloudWatch Log Groups** - For logging

## Setup Instructions

1. **Copy the example variables file:**
   ```bash
   cp terraform.tfvars.example terraform.tfvars
   ```

2. **Edit `terraform.tfvars` with your specific values:**
   ```hcl
   aws_region     = "us-east-1"
   project_name   = "space-trading-sim"
   environment    = "dev"
   
   cognito_callback_urls = [
     "http://localhost:5173",
     "https://your-domain.com"
   ]
   
   cognito_logout_urls = [
     "http://localhost:5173", 
     "https://your-domain.com"
   ]
   
   # Set to true if you want ECS deployment
   enable_ecs = false
   ```

3. **Initialize Terraform:**
   ```bash
   terraform init
   ```

4. **Plan the deployment:**
   ```bash
   terraform plan
   ```

5. **Apply the configuration:**
   ```bash
   terraform apply
   ```

6. **Note the outputs** - These will be needed for your application configuration.

## Important Outputs

After deployment, Terraform will output important values needed for your application:

- `cognito_user_pool_id` - User Pool ID for authentication
- `cognito_user_pool_client_id` - Client ID for the frontend
- `api_gateway_url` - REST API endpoint
- `websocket_api_url` - WebSocket API endpoint
- `aws_config` - Complete configuration object for the frontend

## Configuration for Applications

### Frontend Configuration

Create a file `src/aws-config.js` in your frontend with the Terraform outputs:

```javascript
export const awsConfig = {
  region: "us-east-1",
  userPoolId: "us-east-1_XXXXXXXXX",
  userPoolWebClientId: "xxxxxxxxxxxxxxxxxxxxxxxxxx", 
  identityPoolId: "us-east-1:xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  apiGatewayUrl: "https://xxxxxxxxxx.execute-api.us-east-1.amazonaws.com/dev",
  websocketUrl: "wss://xxxxxxxxxx.execute-api.us-east-1.amazonaws.com/dev",
  cognitoDomain: "https://space-trading-sim-dev-xxxxxxxx.auth.us-east-1.amazoncognito.com"
};
```

### Backend Configuration

Set environment variables for your backend:

```bash
export AWS_REGION=us-east-1
export COGNITO_USER_POOL_ID=us-east-1_XXXXXXXXX
export COGNITO_CLIENT_ID=xxxxxxxxxxxxxxxxxxxxxxxxxx
export API_GATEWAY_ID=xxxxxxxxxx
export WEBSOCKET_API_ID=xxxxxxxxxx
```

## ECS Deployment (Optional)

If you set `enable_ecs = true`, additional resources will be created:

1. **Build and push your backend image:**
   ```bash
   # Get ECR login token
   aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin <account-id>.dkr.ecr.us-east-1.amazonaws.com
   
   # Build and tag image
   cd ../backend
   docker build -t space-trading-sim-dev-backend .
   docker tag space-trading-sim-dev-backend:latest <account-id>.dkr.ecr.us-east-1.amazonaws.com/space-trading-sim-dev-backend:latest
   
   # Push image
   docker push <account-id>.dkr.ecr.us-east-1.amazonaws.com/space-trading-sim-dev-backend:latest
   ```

2. **Deploy ECS service** (you'll need to create ECS service resources separately or use AWS Console)

## Cleanup

To destroy all resources:

```bash
terraform destroy
```

## Security Notes

- User Pool requires email verification
- API Gateway uses Cognito authorizers for protected endpoints
- WebSocket API also uses JWT authorization
- IAM roles follow least privilege principle
- Consider enabling additional security features for production:
  - WAF for API Gateway
  - VPC endpoints
  - Enhanced monitoring

## Customization

You can customize the deployment by:

1. **Adding custom domain** - Set `domain_name` variable and add Route53/ACM resources
2. **Modifying Cognito settings** - Edit `cognito.tf` for different password policies, attributes, etc.
3. **Enable Google Sign-In (Hosted UI)**
    - In `terraform.tfvars`, set:
       ```hcl
       enable_google_idp    = true
       google_client_id     = "<your-google-oauth-client-id>"
       google_client_secret = "<your-google-oauth-client-secret>"
       ```
    - In Google Cloud Console (OAuth consent + Credentials), add Authorized redirect URIs matching your Cognito Hosted UI callback, for example:
       - `https://<your-cognito-domain>.auth.${var.aws_region}.amazoncognito.com/oauth2/idpresponse`
       - And ensure your app callback URLs in `cognito_callback_urls` include your site, e.g. `https://space-trader.click/auth/callback`.
    - Apply Terraform. The Hosted UI will automatically show "Continue with Google"; no frontend changes or buttons needed.
3. **Adding API endpoints** - Create additional API Gateway resources in `api_gateway.tf`
4. **Enhanced monitoring** - Add CloudWatch alarms and dashboards

## Troubleshooting

- Ensure your AWS credentials have sufficient permissions
- Check CloudWatch logs for ECS tasks if using containerized deployment
- Verify Cognito callback URLs match your application URLs
- If Google doesn't appear in the Hosted UI, ensure `enable_google_idp` is true and the Google client ID/secret are set; re-apply Terraform
- API Gateway CORS settings may need adjustment for your frontend domain
