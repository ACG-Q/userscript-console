// Discussion tab switching
(function() {
    var tabs = document.querySelectorAll('#discTabs .tab');
    var contents = document.querySelectorAll('.tab-content');
    tabs.forEach(function(t, i) {
        t.addEventListener('click', function() {
            tabs.forEach(function(x) { x.classList.remove('active'); });
            contents.forEach(function(x) { x.classList.remove('active'); });
            t.classList.add('active');
            contents[i].classList.add('active');
        });
    });
})();
