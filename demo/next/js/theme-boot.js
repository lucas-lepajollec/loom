try {
  var t = localStorage.getItem('loom-theme') || 'dark';
  document.documentElement.dataset.theme = t === 'light' ? 'light' : 'dark';
} catch (e) {}
