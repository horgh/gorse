"use strict";

if (Gorse === undefined) {
	var Gorse = {};
}

Gorse.log = function(msg) {
	if (!window || !window.console || !window.console.log ||
			msg === undefined) {
		return;
	}
	window.console.log(msg);
};

// Toggle between 3 states:
// - none, read, archive
Gorse.toggle_read_state = function(item_li) {
	// Read -> archive.
	if (item_li.classList.contains('read')) {
		item_li.classList.remove('read');
		item_li.classList.add('archive');

		var state_change = item_li.querySelector('.state-change');
		state_change.setAttribute('name', 'archive-item');

		return;
	}

	// Archive -> none.
	if (item_li.classList.contains('archive')) {
		item_li.classList.remove('archive');

		var state_change = item_li.querySelector('.state-change');
		state_change.remove();

		return;
	}

	// None -> read

	item_li.classList.add('read');


	var state_change = document.createElement('input');

	state_change.setAttribute('type', 'hidden');
	state_change.setAttribute('name', 'read-item');

	state_change.classList.add('state-change');

	var id_ele = item_li.querySelector('.item_id');
	state_change.value = id_ele.value

	item_li.appendChild(state_change);
};

// Determine how many items we have selected. Update a displayed counter.
Gorse.update_counts = function() {
	// Count how many we have selected.
	var count = document.querySelectorAll('.state-change').length;

	// Display the counter in the save button.
	var label = 'Save (' + count + ')';

	var save_button = document.getElementById('update-flags-top');
	save_button.textContent = label;
};

document.addEventListener('DOMContentLoaded', function() {
	// Add a click handler to all feed item rows.

	var items = document.querySelectorAll("#items > li");

	for (var i = 0; i < items.length; i++) {
		var li = items.item(i);

		(function(li) {
			li.addEventListener('click', function() {
				Gorse.toggle_read_state(li);
				Gorse.update_counts();
			})
		})(li);
	}

	// When we click the save button, submit the form with our read elements.

	var save_button = document.getElementById('update-flags-top');

	save_button.addEventListener('click', function() {
		var items_form = document.getElementById('list-items-form');

		items_form.submit();
	});
});
