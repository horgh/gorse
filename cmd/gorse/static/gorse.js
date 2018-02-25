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
	if (Gorse.is_read(item_li)) {
		Gorse.set_archive(item_li);
		return;
	}

	if (Gorse.is_archive(item_li)) {
		Gorse.set_none(item_li);
		return;
	}

	Gorse.set_read(item_li);
};

Gorse.is_read = function(item_li) {
	var input = item_li.querySelector('.read-item');
	return input.disabled === false;
};

Gorse.is_archive = function(item_li) {
	var input = item_li.querySelector('.archive-item');
	return input.disabled === false;
};

Gorse.set_read = function(item_li) {
	{
		var input = item_li.querySelector('.read-item');
		input.disabled = false;
		item_li.classList.add('read');
	}
	{
		var input = item_li.querySelector('.archive-item');
		input.disabled = true;
		item_li.classList.remove('archive');
	}
};

Gorse.set_archive = function(item_li) {
	{
		var input = item_li.querySelector('.read-item');
		input.disabled = true;
		item_li.classList.remove('read');
	}
	{
		var input = item_li.querySelector('.archive-item');
		input.disabled = false;
		item_li.classList.add('archive');
	}
};

Gorse.set_none = function(item_li) {
	{
		var input = item_li.querySelector('.read-item');
		input.disabled = true;
		item_li.classList.remove('read');
	}
	{
		var input = item_li.querySelector('.archive-item');
		input.disabled = true;
		item_li.classList.remove('archive');
	}
};

document.addEventListener('DOMContentLoaded', function() {
	// Add a click handler to all item rows.

	var items = document.querySelectorAll("#items > li");

	for (var i = 0; i < items.length; i++) {
		var li = items.item(i);

		(function(li) {
			li.addEventListener('click', function() {
				Gorse.toggle_read_state(li);
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
