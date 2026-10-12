import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { api, tokenStore } from './api'

const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const [user, setUser] = useState(null)
  const [ready, setReady] = useState(false)

  const logout = useCallback(() => {
    tokenStore.clear()
    setUser(null)
  }, [])

  // Restore the session on first load.
  useEffect(() => {
    let alive = true
    if (!tokenStore.get()) {
      setReady(true)
      return
    }
    api
      .me()
      .then((r) => alive && setUser(r.user))
      .catch(() => tokenStore.clear())
      .finally(() => alive && setReady(true))
    return () => {
      alive = false
    }
  }, [])

  // Any 401 from an authenticated call (expired token) logs the user out everywhere.
  useEffect(() => {
    window.addEventListener('livepoll:unauthorized', logout)
    return () => window.removeEventListener('livepoll:unauthorized', logout)
  }, [logout])

  const finish = (r) => {
    tokenStore.set(r.token)
    setUser(r.user)
  }

  const value = useMemo(
    () => ({
      user,
      ready,
      logout,
      login: async (email, password) => finish(await api.login({ email, password })),
      signup: async (name, email, password) => finish(await api.signup({ name, email, password })),
    }),
    [user, ready, logout],
  )
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export const useAuth = () => useContext(AuthContext)
