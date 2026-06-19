import { useEffect, useMemo, useState } from 'react'
import {
  Avatar,
  Button,
  Card,
  CapsuleTabs,
  Form,
  Input,
  NavBar,
  SafeArea,
  Space,
  TabBar,
  Toast,
} from 'antd-mobile'
import {
  AppOutline,
  CheckShieldOutline,
  KeyOutline,
  PhonebookOutline,
  UserOutline,
} from 'antd-mobile-icons'
import type { ClientProfile } from './api/clientAuth'
import {
  getClientProfile,
  loginClientUser,
  logoutClientUser,
  registerClientUser,
} from './api/clientAuth'
import { clearClientToken, getClientToken, setClientToken } from './auth/token'

type AuthMode = 'login' | 'register'
type MainTab = 'home' | 'mine'

interface LoginValues {
  phone: string
  password: string
}

interface RegisterValues extends LoginValues {
  nickname: string
}

const clientId = import.meta.env.VITE_CLIENT_ID || 'h5-client'

export default function App() {
  const [authMode, setAuthMode] = useState<AuthMode>('login')
  const [activeTab, setActiveTab] = useState<MainTab>('home')
  const [profile, setProfile] = useState<ClientProfile | null>(null)
  const [booting, setBooting] = useState(true)
  const [submitting, setSubmitting] = useState(false)

  const isAuthed = !!profile

  useEffect(() => {
    const token = getClientToken()
    if (!token) {
      setBooting(false)
      return
    }
    getClientProfile()
      .then(setProfile)
      .catch(() => {
        clearClientToken()
      })
      .finally(() => setBooting(false))
  }, [])

  const title = useMemo(() => {
    if (!isAuthed) {
      return authMode === 'login' ? '登录' : '注册'
    }
    return activeTab === 'home' ? '首页' : '我的'
  }, [activeTab, authMode, isAuthed])

  async function refreshProfile() {
    const nextProfile = await getClientProfile()
    setProfile(nextProfile)
  }

  async function handleLogin(values: LoginValues) {
    setSubmitting(true)
    try {
      const resp = await loginClientUser({ ...values, clientId })
      setClientToken(resp.token)
      await refreshProfile()
      setActiveTab('home')
      Toast.show({ content: '登录成功' })
    } catch (error) {
      Toast.show({ content: error instanceof Error ? error.message : '登录失败' })
    } finally {
      setSubmitting(false)
    }
  }

  async function handleRegister(values: RegisterValues) {
    setSubmitting(true)
    try {
      const resp = await registerClientUser({ ...values, clientId })
      setClientToken(resp.token)
      await refreshProfile()
      setActiveTab('home')
      Toast.show({ content: '注册成功' })
    } catch (error) {
      Toast.show({ content: error instanceof Error ? error.message : '注册失败' })
    } finally {
      setSubmitting(false)
    }
  }

  async function handleLogout() {
    try {
      await logoutClientUser()
    } finally {
      clearClientToken()
      setProfile(null)
      setActiveTab('home')
      setAuthMode('login')
    }
  }

  return (
    <div className="app-shell">
      <SafeArea position="top" />
      <NavBar back={null} className="app-nav">
        {title}
      </NavBar>
      <main className="app-main">
        {booting ? (
          <div className="center-state">加载中</div>
        ) : isAuthed && profile ? (
          activeTab === 'home' ? <HomeScreen /> : <MineScreen profile={profile} onLogout={handleLogout} />
        ) : (
          <AuthScreen
            mode={authMode}
            submitting={submitting}
            onModeChange={setAuthMode}
            onLogin={handleLogin}
            onRegister={handleRegister}
          />
        )}
      </main>
      {isAuthed ? (
        <TabBar activeKey={activeTab} onChange={(key) => setActiveTab(key as MainTab)} className="app-tabbar">
          <TabBar.Item key="home" icon={<AppOutline />} title="首页" />
          <TabBar.Item key="mine" icon={<UserOutline />} title="我的" />
        </TabBar>
      ) : null}
      <SafeArea position="bottom" />
    </div>
  )
}

function AuthScreen(props: {
  mode: AuthMode
  submitting: boolean
  onModeChange: (mode: AuthMode) => void
  onLogin: (values: LoginValues) => Promise<void>
  onRegister: (values: RegisterValues) => Promise<void>
}) {
  return (
    <section className="auth-screen">
      <div className="auth-heading">
        <div className="auth-mark"><CheckShieldOutline /></div>
        <h1>RBAC Client</h1>
        <p>独立 ToC 用户体系</p>
      </div>

      <CapsuleTabs activeKey={props.mode} onChange={(key) => props.onModeChange(key as AuthMode)}>
        <CapsuleTabs.Tab title="登录" key="login" />
        <CapsuleTabs.Tab title="注册" key="register" />
      </CapsuleTabs>

      {props.mode === 'login' ? (
        <Form
          layout="vertical"
          mode="card"
          footer={(
            <Button block color="primary" size="large" type="submit" loading={props.submitting}>
              登录
            </Button>
          )}
          onFinish={(values) => props.onLogin(values as LoginValues)}
        >
          <Form.Item name="phone" label="手机号" rules={[{ required: true, message: '请输入手机号' }]}>
            <Input placeholder="请输入手机号" clearable inputMode="tel" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
            <Input placeholder="请输入密码" clearable type="password" />
          </Form.Item>
        </Form>
      ) : (
        <Form
          layout="vertical"
          mode="card"
          footer={(
            <Button block color="primary" size="large" type="submit" loading={props.submitting}>
              注册
            </Button>
          )}
          onFinish={(values) => props.onRegister(values as RegisterValues)}
        >
          <Form.Item name="phone" label="手机号" rules={[{ required: true, message: '请输入手机号' }]}>
            <Input placeholder="请输入手机号" clearable inputMode="tel" />
          </Form.Item>
          <Form.Item name="nickname" label="昵称" rules={[{ required: true, message: '请输入昵称' }]}>
            <Input placeholder="请输入昵称" clearable />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
            <Input placeholder="至少 6 位" clearable type="password" />
          </Form.Item>
        </Form>
      )}
    </section>
  )
}

function HomeScreen() {
  return (
    <section className="home-screen">
      <Card className="summary-card">
        <Space direction="vertical" block>
          <div className="summary-title">RBAC + ToC 快速启动</div>
          <div className="summary-copy">后台权限与 C 端会员登录已经隔离，可以直接扩展订单、内容、积分或会员业务。</div>
        </Space>
      </Card>
      <div className="feature-grid">
        <FeatureCard icon={<PhonebookOutline />} title="独立会员" desc="client_user 表承载 C 端账号" />
        <FeatureCard icon={<KeyOutline />} title="独立令牌" desc="client subject 防止串用后台权限" />
        <FeatureCard icon={<CheckShieldOutline />} title="后台 RBAC" desc="管理端继续使用角色菜单权限" />
      </div>
    </section>
  )
}

function FeatureCard(props: { icon: React.ReactNode; title: string; desc: string }) {
  return (
    <Card className="feature-card">
      <div className="feature-icon">{props.icon}</div>
      <div className="feature-title">{props.title}</div>
      <div className="feature-desc">{props.desc}</div>
    </Card>
  )
}

function MineScreen(props: { profile: ClientProfile; onLogout: () => Promise<void> }) {
  const initial = props.profile.nickname.slice(0, 1) || 'C'
  return (
    <section className="mine-screen">
      <Card className="profile-card">
        <div className="profile-row">
          <Avatar src={props.profile.avatar} fallback={initial} className="profile-avatar" />
          <div className="profile-meta">
            <div className="profile-name">{props.profile.nickname}</div>
            <div className="profile-phone">{props.profile.phone}</div>
          </div>
        </div>
      </Card>
      <Card className="account-card">
        <div className="account-line">
          <span>用户体系</span>
          <strong>client_user</strong>
        </div>
        <div className="account-line">
          <span>客户端</span>
          <strong>H5</strong>
        </div>
        <div className="account-line">
          <span>登录态</span>
          <strong>client_token</strong>
        </div>
      </Card>
      <Button block size="large" color="danger" fill="outline" onClick={props.onLogout}>
        退出登录
      </Button>
    </section>
  )
}
