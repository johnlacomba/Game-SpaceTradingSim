import React, { createContext, useContext, useEffect, useState } from 'react';
import { Amplify } from 'aws-amplify';
import { 
  getCurrentUser, 
  signIn, 
  signOut, 
  signUp, 
  confirmSignUp, 
  resendSignUpCode,
  fetchAuthSession,
  fetchUserAttributes
} from 'aws-amplify/auth';
import awsConfig from '../aws-config.js';

// Check if we're in development mode
const isDevMode = process.env.NODE_ENV !== 'production' && import.meta.env.VITE_USE_AWS_AUTH !== 'true';

console.log('Auth mode check:', { 
  NODE_ENV: process.env.NODE_ENV, 
  VITE_USE_AWS_AUTH: import.meta.env.VITE_USE_AWS_AUTH,
  isDevMode 
});

// Configure Amplify only in production mode or when explicitly enabled
if (!isDevMode) {
  console.log('Configuring Amplify with:', awsConfig);
  try {
    Amplify.configure({
      Auth: {
        Cognito: {
          userPoolId: awsConfig.userPoolId,
          userPoolClientId: awsConfig.userPoolWebClientId,
          identityPoolId: awsConfig.identityPoolId,
          loginWith: {
            oauth: awsConfig.oauth,
            username: true,
            email: true
          }
        }
      }
    });
    console.log('Amplify configured successfully');
  } catch (error) {
    console.error('Error configuring Amplify:', error);
  }
} else {
  console.log('Using development mode - Amplify not configured');
}

interface User {
  username: string;
  email: string;
  name: string;
  sub: string;
}

interface AuthContextType {
  user: User | null;
  loading: boolean;
  signIn: (username: string, password: string) => Promise<any>;
  signUp: (username: string, password: string, email: string, name: string) => Promise<any>;
  signOut: () => Promise<void>;
  confirmSignUp: (username: string, code: string) => Promise<any>;
  resendConfirmationCode: (username: string) => Promise<any>;
  getAccessToken: () => Promise<string | null>;
}

const AuthContext = createContext<AuthContextType>({} as AuthContextType);

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};

interface AuthProviderProps {
  children: React.ReactNode;
}

export const AuthProvider: React.FC<AuthProviderProps> = ({ children }) => {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  // Mock functions for development mode
  const mockGetAccessToken = async (): Promise<string> => {
    return 'mock-jwt-token';
  };

  const mockCheckAuthState = async () => {
    // In dev mode, automatically sign in with a mock user
    const mockUser: User = {
      username: 'devuser',
      email: 'dev@example.com',
      name: 'Development User',
      sub: 'dev-user-123'
    };
    setUser(mockUser);
    setLoading(false);
  };

  const mockSignIn = async (username: string, password: string) => {
    const mockUser: User = {
      username: username,
      email: `${username}@example.com`,
      name: username,
      sub: `mock-${username}-123`
    };
    setUser(mockUser);
    return { isSignInComplete: true };
  };

  const mockSignUp = async (username: string, password: string, email: string, name: string) => {
    // In dev mode, immediately complete signup
    const mockUser: User = {
      username: username,
      email: email,
      name: name,
      sub: `mock-${username}-123`
    };
    setUser(mockUser);
    return { isSignUpComplete: true };
  };

  const mockSignOut = async () => {
    setUser(null);
  };

  const mockConfirmSignUp = async (username: string, code: string) => {
    return { isSignUpComplete: true };
  };

  const mockResendConfirmationCode = async (username: string) => {
    return { success: true };
  };

  const getAccessToken = async (): Promise<string | null> => {
    if (isDevMode) return mockGetAccessToken();
    try {
      const session = await fetchAuthSession();
      return session.tokens?.accessToken?.toString() || null;
    } catch (error) {
      console.error('Error getting access token:', error);
      return null;
    }
  };

  const checkAuthState = async () => {
    console.log('Checking auth state, isDevMode:', isDevMode);
    if (isDevMode) return mockCheckAuthState();
    
    try {
      // Add timeout to prevent hanging
      const timeoutPromise = new Promise((_, reject) => 
        setTimeout(() => reject(new Error('Auth check timeout')), 5000)
      );
      
      const authPromise = getCurrentUser();
      const currentUser = await Promise.race([authPromise, timeoutPromise]) as any;
      
      console.log('Current user:', currentUser);
      if (currentUser) {
        const userAttributes = await fetchUserAttributes();
        console.log('User attributes:', userAttributes);
        setUser({
          username: currentUser.username,
          email: userAttributes.email || '',
          name: userAttributes.name || userAttributes.email || currentUser.username,
          sub: userAttributes.sub || ''
        });
      } else {
        setUser(null);
      }
    } catch (error) {
      console.log('No authenticated user found or auth error:', error);
      setUser(null);
    } finally {
      console.log('Setting loading to false');
      setLoading(false);
    }
  };

  useEffect(() => {
    checkAuthState();
  }, []);

  const handleSignIn = async (username: string, password: string) => {
    if (isDevMode) return mockSignIn(username, password);
    try {
      const result = await signIn({ username, password });
      await checkAuthState(); // Refresh user state
      return result;
    } catch (error) {
      console.error('Sign in error:', error);
      throw error;
    }
  };

  const handleSignUp = async (username: string, password: string, email: string, name: string) => {
    if (isDevMode) return mockSignUp(username, password, email, name);
    try {
      const result = await signUp({
        username,
        password,
        options: {
          userAttributes: {
            email,
            name
          }
        }
      });
      return result;
    } catch (error) {
      console.error('Sign up error:', error);
      throw error;
    }
  };

  const handleSignOut = async () => {
    if (isDevMode) return mockSignOut();
    try {
      await signOut();
      setUser(null);
    } catch (error) {
      console.error('Sign out error:', error);
      throw error;
    }
  };

  const handleConfirmSignUp = async (username: string, code: string) => {
    if (isDevMode) return mockConfirmSignUp(username, code);
    try {
      const result = await confirmSignUp({ username, confirmationCode: code });
      return result;
    } catch (error) {
      console.error('Confirm sign up error:', error);
      throw error;
    }
  };

  const handleResendConfirmationCode = async (username: string) => {
    if (isDevMode) return mockResendConfirmationCode(username);
    try {
      const result = await resendSignUpCode({ username });
      return result;
    } catch (error) {
      console.error('Resend confirmation code error:', error);
      throw error;
    }
  };

  const value = {
    user,
    loading,
    signIn: handleSignIn,
    signUp: handleSignUp,
    signOut: handleSignOut,
    confirmSignUp: handleConfirmSignUp,
    resendConfirmationCode: handleResendConfirmationCode,
    getAccessToken
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};
