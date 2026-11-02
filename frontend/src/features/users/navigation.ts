export const userPages = [
  {id: 'dashboard', name: '仪表盘', hint: '用量趋势与模型使用情况', icon: 'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z'},
  {id: 'keys', name: '我的密钥', hint: '客户端接入与个人 API 密钥', icon: 'M11 12h10m-4 0v3m3-3v2M11 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z'},
  {id: 'models', name: '可用模型', hint: '当前分组可使用的模型目录', icon: 'm12 3 9 5-9 5-9-5 9-5ZM3 8v9l9 5 9-5V8M12 13v9'},
  {id: 'usage', name: '我的用量', hint: '个人请求记录与费用明细', icon: 'M4 20V10h4v10m4 0V4h4v16m4 0V7h3v13'},
  {id: 'entitlements', name: '额度与兑换', hint: '余额、订阅与兑换记录', icon: 'M3 6h18v14H3zM3 10h18m-6 5h3M6 3h12'},
  {id: 'identity', name: '登录方式', hint: '管理邮箱与第三方账号绑定', icon: 'M12 3 4 6v6c0 4 3 7 8 9 5-2 8-5 8-9V6l-8-3Zm-4 9 3 3 5-6'},
  {id: 'profile', name: '个人资料', hint: '维护展示名称与头像', icon: 'M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0ZM4 21v-2a8 8 0 0 1 16 0v2'},
  {id: 'password', name: '修改密码', hint: '更新密码与登录安全', icon: 'M6 10h12v11H6zM8 10V6a4 4 0 0 1 8 0v4m-4 4v3'},
] as const;
export type UserPage = typeof userPages[number]['id'];
export function userPageFromHash(): UserPage {
  const id = location.hash.slice(1);
  return userPages.find(page => page.id === id)?.id ?? 'dashboard';
}
