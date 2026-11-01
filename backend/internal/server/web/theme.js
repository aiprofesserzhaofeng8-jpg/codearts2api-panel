(function(){
  "use strict";
  var theme = "dark";
  try {
    var saved = localStorage.getItem("codearts2api_theme");
    if (saved === "light" || saved === "dark") theme = saved;
    else if (window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches) theme = "light";
  } catch(e){}
  document.documentElement.dataset.theme = theme;
})();
