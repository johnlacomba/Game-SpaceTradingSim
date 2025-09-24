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
import { handleSignIn } from 'aws-amplify/auth';
import awsConfig from '../aws-config.js';

// Check if we're in development mode
const isDevMode = process.env.NODE_ENV !== 'production' && import.meta.env.VITE_USE_AWS_AUTH !== 'true';

//

// Configure Amplify only in production mode or when explicitly enabled
if (!isDevMode) {
  // Configure Amplify
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
  // Amplify configured
  } catch (error) {
    console.error('Error configuring Amplify:', error);
  }
} else {
  // Dev mode: Amplify not configured
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

  // Ensure Hosted UI callback is processed and URL cleaned
  useEffect(() => {
    const isCallback = window.location.pathname.startsWith('/auth/callback')
    if (!isDevMode && isCallback) {
      // Complete the Hosted UI redirect flow (exchanges code for tokens)
      const complete = async () => {
        try {
          const hasCode = /[?&]code=/.test(window.location.search)
          // Callback detected
          // Exchange code for tokens
          await fetchAuthSession()
          // Session fetched after callback
          // Attempt to populate user immediately
          try {
            const currentUser = await getCurrentUser()
            const attrs = await fetchUserAttributes()
            setUser({
              username: currentUser.username,
              email: attrs.email || '',
              name: attrs.name || attrs.email || currentUser.username,
              sub: attrs.sub || ''
            })
            // User set after callback
          } catch (e) {
            // getCurrentUser after callback failed
          }
        } catch (e) {
          // Callback processing error
        } finally {
          // Clean the URL
          const target = window.location.origin + '/'
          window.history.replaceState({}, document.title, target)
        }
      }
      void complete()
    }
  }, [])

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
  // Check auth state
    if (isDevMode) return mockCheckAuthState();
    
    try {
      // Add timeout to prevent hanging
      const timeoutPromise = new Promise((_, reject) => 
        setTimeout(() => reject(new Error('Auth check timeout')), 5000)
      );
      
      const authPromise = getCurrentUser();
      const currentUser = await Promise.race([authPromise, timeoutPromise]) as any;
      
  // Current user fetched
      if (currentUser) {
        const userAttributes = await fetchUserAttributes();
  // User attributes fetched
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
  // No authenticated user
      setUser(null);
    } finally {
  // Loading complete
      setLoading(false);
    }
  };

  useEffect(() => {
    // First process any hosted UI callback, then check auth
    const doCheck = async () => {
      try {
        await fetchAuthSession()
      } catch {}
      await checkAuthState()
    }
    doCheck()
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
