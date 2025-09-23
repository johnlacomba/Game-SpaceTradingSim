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
const isDevMode = process.env.NODE_ENV !== 'production';

// Configure Amplify only in production mode
if (!isDevMode) {
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
    if (isDevMode) return mockCheckAuthState();
    try {
      const currentUser = await getCurrentUser();
      if (currentUser) {
        const userAttributes = await fetchUserAttributes();
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
      console.log('No authenticated user found');
      setUser(null);
    } finally {
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
