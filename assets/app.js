const pages = {
  dashboard: { title: 'Dashboard', breadcrumb: 'Dashboard' },
  models: { title: '模型管理', breadcrumb: 'Models' },
  providers: { title: '提供商', breadcrumb: 'Providers' },
  logs: { title: '活动日志', breadcrumb: 'Logs' },
  settings: { title: '设置', breadcrumb: 'Settings' },
  'api-keys': { title: 'API Keys', breadcrumb: 'API Keys' },
  usage: { title: '用量', breadcrumb: 'Usage' }
};

const sidebar = document.getElementById('sidebar');
const overlay = document.getElementById('overlay');
const openSidebarBtn = document.getElementById('openSidebar');
const pageTitle = document.getElementById('pageTitle');
const breadcrumbCurrent = document.getElementById('breadcrumbCurrent');
const pageEls = document.querySelectorAll('.page');
const navItems = document.querySelectorAll('.nav-item[data-page]');
const tabItems = document.querySelectorAll('.mobile-tabbar .tab-item[data-page]');
const bottomSheet = document.getElementById('bottomSheet');
const sheetTitle = document.getElementById('sheetTitle');
const sheetBody = document.getElementById('sheetBody');

function closeSidebar() {
  sidebar.classList.remove('open');
  overlay.classList.remove('show');
}

openSidebarBtn?.addEventListener('click', () => {
  sidebar.classList.add('open');
  overlay.classList.add('show');
});

overlay?.addEventListener('click', closeSidebar);

function navigateTo(page) {
  const meta = pages[page] || pages.dashboard;
  pageTitle.textContent = meta.title;
  breadcrumbCurrent.textContent = meta.breadcrumb;

  pageEls.forEach((el) => {
    el.classList.toggle('active', el.id === `page-${page}`);
  });

  navItems.forEach((item) => {
    item.classList.toggle('active', item.dataset.page === page);
  });

  tabItems.forEach((item) => {
    item.classList.toggle('active', item.dataset.page === page);
  });

  closeSidebar();
}

navItems.forEach((item) => {
  item.addEventListener('click', () => navigateTo(item.dataset.page));
});

tabItems.forEach((item) => {
  item.addEventListener('click', () => navigateTo(item.dataset.page));
});

document.querySelectorAll('[data-link]').forEach((el) => {
  el.addEventListener('click', () => navigateTo(el.dataset.link));
});

// Bottom sheet helper
const sheetData = {
  glm: { title: 'GLM · glm-5-turbo 详情', body: [['模型', 'glm-5-turbo'], ['状态', '在线'], ['今日请求', '48.2K'], ['平均延迟', '0.8s'], ['成功率', '99.92%']] },
  mimo: { title: 'MiMo · mimo-v2.5 详情', body: [['模型', 'mimo-v2.5'], ['状态', '在线'], ['今日请求', '32.1K'], ['平均延迟', '1.1s'], ['成功率', '99.81%']] },
  deepv: { title: 'DeepV · deepseek-v4-pro 详情', body: [['模型', 'deepseek-v4-pro'], ['状态', '在线'], ['今日请求', '22.7K'], ['平均延迟', '0.6s'], ['成功率', '99.95%']] }
};

export function openSheet(key) {
  const data = sheetData[key];
  if (!data) return;
  sheetTitle.textContent = data.title;
  sheetBody.innerHTML = data.body.map(([label, value]) => `<div class="stat-row"><span class="stat-label">${label}</span><span class="stat-value">${value}</span></div>`).join('');
  bottomSheet.classList.add('open');
  overlay.classList.add('show');
}

export function closeSheet() {
  bottomSheet.classList.remove('open');
  overlay.classList.remove('show');
}

// expose for inline handlers
window.openSheet = openSheet;
window.closeSheet = closeSheet;

// Attach provider chip click behavior
document.querySelectorAll('.provider-chip').forEach((chip) => {
  chip.addEventListener('click', () => {
    const label = chip.querySelector('.p-label')?.textContent?.trim();
    const key = label?.toLowerCase().replace(/\s+/g, '');
    openSheet(key);
  });
});

// Initial route
navigateTo('dashboard');
