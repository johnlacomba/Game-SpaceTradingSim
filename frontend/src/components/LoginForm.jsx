import React, { useState } from 'react';
import { useAuth } from '../contexts/AuthContext.jsx';

const LoginForm = ({ onClose }) => {
  const [isSignUp, setIsSignUp] = useState(false);
  const [isConfirming, setIsConfirming] = useState(false);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [confirmationCode, setConfirmationCode] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const { signIn, signUp, confirmSignUp, resendConfirmationCode } = useAuth();

  console.log('LoginForm rendered', { isSignUp, isConfirming });

  const handleSubmit = async (e) => {
    e.preventDefault();
    console.log('Form submitted', { isSignUp, isConfirming, username });
    setLoading(true);
    setError('');

    try {
      if (isConfirming) {
        console.log('Confirming signup...');
        await confirmSignUp(username, confirmationCode);
        setIsConfirming(false);
        setIsSignUp(false);
        // Automatically sign in after confirmation
        await signIn(username, password);
        onClose();
      } else if (isSignUp) {
        console.log('Signing up...');
        const result = await signUp(username, password, email, name);
        if (result.isSignUpComplete) {
          await signIn(username, password);
          onClose();
        } else {
          setIsConfirming(true);
        }
      } else {
        console.log('Signing in...');
        await signIn(username, password);
        onClose();
      }
    } catch (err) {
      console.error('Auth error:', err);
      setError(err.message || 'An error occurred');
    } finally {
      setLoading(false);
    }
  };

  const handleResendCode = async () => {
    try {
      await resendConfirmationCode(username);
      setError('Confirmation code resent to your email');
    } catch (err) {
      setError(err.message || 'Failed to resend code');
    }
  };

  return (
    <div style={{ 
      position: 'fixed', 
      top: 0, 
      left: 0, 
      right: 0, 
      bottom: 0, 
      backgroundColor: 'rgba(0,0,0,0.8)', 
      display: 'flex', 
      justifyContent: 'center', 
      alignItems: 'center',
      zIndex: 1000
    }}>
      <div style={{ 
        backgroundColor: '#1a1a2e', 
        padding: '2rem', 
        borderRadius: '8px', 
        border: '1px solid #16213e',
        minWidth: '400px',
        color: '#eee'
      }}>
        <h2 style={{ marginBottom: '1rem', textAlign: 'center' }}>
          {isConfirming ? 'Confirm Email' : isSignUp ? 'Sign Up' : 'Sign In'}
        </h2>
        
        {error && (
          <div style={{ 
            color: '#ff6b6b', 
            marginBottom: '1rem', 
            padding: '0.5rem', 
            backgroundColor: 'rgba(255,107,107,0.1)',
            borderRadius: '4px'
          }}>
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit}>
          {isConfirming ? (
            <>
              <div style={{ marginBottom: '1rem' }}>
                <label style={{ display: 'block', marginBottom: '0.5rem' }}>
                  Confirmation Code
                </label>
                <input
                  type="text"
                  value={confirmationCode}
                  onChange={(e) => setConfirmationCode(e.target.value)}
                  required
                  style={{
                    width: '100%',
                    padding: '0.5rem',
                    backgroundColor: '#16213e',
                    border: '1px solid #0f3460',
                    borderRadius: '4px',
                    color: '#eee'
                  }}
                  placeholder="Enter confirmation code"
                />
              </div>
              <button
                type="button"
                onClick={handleResendCode}
                style={{
                  width: '100%',
                  padding: '0.5rem',
                  marginBottom: '1rem',
                  backgroundColor: 'transparent',
                  border: '1px solid #0f3460',
                  borderRadius: '4px',
                  color: '#0f3460',
                  cursor: 'pointer'
                }}
              >
                Resend Code
              </button>
            </>
          ) : (
            <>
              <div style={{ marginBottom: '1rem' }}>
                <label style={{ display: 'block', marginBottom: '0.5rem' }}>
                  Username
                </label>
                <input
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  required
                  style={{
                    width: '100%',
                    padding: '0.5rem',
                    backgroundColor: '#16213e',
                    border: '1px solid #0f3460',
                    borderRadius: '4px',
                    color: '#eee'
                  }}
                />
              </div>

              {isSignUp && (
                <>
                  <div style={{ marginBottom: '1rem' }}>
                    <label style={{ display: 'block', marginBottom: '0.5rem' }}>
                      Email
                    </label>
                    <input
                      type="email"
                      value={email}
                      onChange={(e) => setEmail(e.target.value)}
                      required
                      style={{
                        width: '100%',
                        padding: '0.5rem',
                        backgroundColor: '#16213e',
                        border: '1px solid #0f3460',
                        borderRadius: '4px',
                        color: '#eee'
                      }}
                    />
                  </div>

                  <div style={{ marginBottom: '1rem' }}>
                    <label style={{ display: 'block', marginBottom: '0.5rem' }}>
                      Display Name
                    </label>
                    <input
                      type="text"
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      required
                      style={{
                        width: '100%',
                        padding: '0.5rem',
                        backgroundColor: '#16213e',
                        border: '1px solid #0f3460',
                        borderRadius: '4px',
                        color: '#eee'
                      }}
                    />
                  </div>
                </>
              )}

              <div style={{ marginBottom: '1rem' }}>
                <label style={{ display: 'block', marginBottom: '0.5rem' }}>
                  Password
                </label>
                <input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  style={{
                    width: '100%',
                    padding: '0.5rem',
                    backgroundColor: '#16213e',
                    border: '1px solid #0f3460',
                    borderRadius: '4px',
                    color: '#eee'
                  }}
                />
              </div>
            </>
          )}

          <button
            type="submit"
            disabled={loading}
            style={{
              width: '100%',
              padding: '0.75rem',
              backgroundColor: '#0f3460',
              border: 'none',
              borderRadius: '4px',
              color: '#eee',
              cursor: loading ? 'not-allowed' : 'pointer',
              marginBottom: '1rem'
            }}
          >
            {loading ? 'Loading...' : (isConfirming ? 'Confirm' : isSignUp ? 'Sign Up' : 'Sign In')}
          </button>
        </form>

        {!isConfirming && (
          <div style={{ textAlign: 'center' }}>
            <button
              type="button"
              onClick={() => setIsSignUp(!isSignUp)}
              style={{
                backgroundColor: 'transparent',
                border: 'none',
                color: '#0f3460',
                cursor: 'pointer',
                textDecoration: 'underline'
              }}
            >
              {isSignUp ? 'Already have an account? Sign In' : 'Need an account? Sign Up'}
            </button>
          </div>
        )}

        <div style={{ textAlign: 'center', marginTop: '1rem' }}>
          <button
            type="button"
            onClick={onClose}
            style={{
              backgroundColor: 'transparent',
              border: 'none',
              color: '#666',
              cursor: 'pointer'
            }}
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
};

export default LoginForm;
