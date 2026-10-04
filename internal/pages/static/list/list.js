// List pagination
(function() {
    var batch = parseInt(document.getElementById('scriptList')?.dataset.batch || 10);
    var total = parseInt(document.getElementById('scriptList')?.dataset.total || 0);
    var shown = 0;
    var cards = document.querySelectorAll('.card');
    
    function loadMore() {
        var next = Math.min(shown + batch, total);
        for (var i = shown; i < next; i++) {
            if (cards[i]) cards[i].style.display = '';
        }
        shown = next;
        var btn = document.getElementById('loadMoreBtn');
        var sentinel = document.getElementById('listSentinel');
        if (btn) btn.style.display = shown >= total ? 'none' : '';
        if (sentinel) sentinel.style.display = shown >= total ? 'none' : '';
    }
    
    var btn = document.getElementById('loadMoreBtn');
    if (btn) btn.addEventListener('click', loadMore);
    
    // IntersectionObserver for infinite scroll fallback
    if (window.IntersectionObserver && document.getElementById('listSentinel')) {
        var observer = new IntersectionObserver(function(entries) {
            if (entries[0].isIntersecting && shown < total) loadMore();
        });
        observer.observe(document.getElementById('listSentinel'));
    }
    
    loadMore();
})();
