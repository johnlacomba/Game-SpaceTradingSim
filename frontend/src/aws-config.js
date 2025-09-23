// AWS Configuration
// Updated with real Terraform output values

export const awsConfig = {
  // AWS region
  region: import.meta.env.VITE_AWS_REGION || 'us-east-1',
  
  // Cognito User Pool configuration
  userPoolId: import.meta.env.VITE_COGNITO_USER_POOL_ID || 'us-east-1_EI39ptGfg',
  userPoolWebClientId: import.meta.env.VITE_COGNITO_CLIENT_ID || '67uon7lhabu3qfh6lchjru523k',
  identityPoolId: import.meta.env.VITE_COGNITO_IDENTITY_POOL_ID || 'us-east-1:e413919c-e752-4e38-a031-5753e3031f0c',
  
  // API configuration
  apiGatewayUrl: import.meta.env.VITE_API_GATEWAY_URL || 'https://vliyfb5w4l.execute-api.us-east-1.amazonaws.com/dev',
  websocketUrl: import.meta.env.VITE_WEBSOCKET_URL || 'wss://flwxzhxehj.execute-api.us-east-1.amazonaws.com/dev',
  
  // Cognito domain for hosted UI
  cognitoDomain: import.meta.env.VITE_COGNITO_DOMAIN || 'space-trading-sim-dev-z3xqj6c6.auth.us-east-1.amazoncognito.com',
  
  // OAuth configuration
  oauth: {
    domain: import.meta.env.VITE_COGNITO_DOMAIN || 'space-trading-sim-dev-z3xqj6c6.auth.us-east-1.amazoncognito.com',
  scopes: ['email', 'openid', 'profile'],
  redirectSignIn: [import.meta.env.VITE_COGNITO_CALLBACK_URL || window.location.origin],
  redirectSignOut: [import.meta.env.VITE_COGNITO_LOGOUT_URL || window.location.origin],
    responseType: 'code'
  }
};

// Log configuration in non-production environments for debugging
if (import.meta.env.DEV || import.meta.env.VITE_USE_AWS_AUTH === 'true') {
  console.log('AWS Config:', awsConfig);
}

// Development fallback configuration
if (import.meta.env.DEV && import.meta.env.VITE_USE_AWS_AUTH !== 'true') {
  // Use localhost for development
  if (!awsConfig.apiGatewayUrl) {
    awsConfig.apiGatewayUrl = 'http://localhost:8080/api';
  }
  if (!awsConfig.websocketUrl) {
    awsConfig.websocketUrl = 'ws://localhost:8080/ws';
  }
}

export default awsConfig;
